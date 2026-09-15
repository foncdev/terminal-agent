// terminal-agent는 맥·리눅스·윈도우의 셸을 HTTP/WebSocket으로 연다.
//
// 기본으로 콘솔 화면이 함께 뜬다. 맥 앞에서는 그냥 쓰고, 나가서는
// 웹으로 같은 화면을 이어 본다. 서버로만 돌리려면 --headless.
//
// 주의: 셸을 여는 순간 이 프로세스는 사실상 그 계정으로 무엇이든 할 수 있다.
// 시작 디렉터리 제한은 실수를 줄이는 장치이지 보안 경계가 아니다.
// 인터넷에 직접 열지 말 것.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/foncdev/terminal-agent/internal/api"
	"github.com/foncdev/terminal-agent/internal/config"
	"github.com/foncdev/terminal-agent/internal/pty"
	"github.com/foncdev/terminal-agent/internal/relaylink"
	"github.com/foncdev/terminal-agent/internal/terminal"
	"github.com/foncdev/terminal-agent/internal/tui"
)

// version은 빌드할 때 주입된다(-X main.version=...).
// 직접 빌드하면 dev로 남는다.
var version = "dev"

func main() {
	headless := flag.Bool("headless", false, "콘솔 화면 없이 서버로만 돈다")
	showVersion := flag.Bool("version", false, "버전을 찍고 끝낸다")
	flag.Parse()

	if *showVersion {
		fmt.Printf("terminal-agent %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	log.SetFlags(log.Ltime)

	cfg := config.Load()

	// 켜야만 뜬다. 실수로 도는 일이 없게 한다.
	if os.Getenv("TERMINAL_ENABLED") != "true" {
		log.Println("[terminal] TERMINAL_ENABLED=true 가 아니라서 시작하지 않습니다.")
		log.Println("[terminal] 셸을 여는 서비스이므로 명시적으로 켜야 합니다.")
		os.Exit(1)
	}

	reg := terminal.NewRegistry(terminal.RegistryOptions{
		Max:         cfg.MaxTerminals,
		Scrollback:  cfg.Scrollback,
		IdleTimeout: cfg.IdleTimeout,
	})
	defer reg.CloseAll()

	srv := &http.Server{
		Addr:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler: api.NewServer(cfg, reg),
		// 스트리밍이 있으므로 쓰기 타임아웃은 두지 않는다.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 화면을 띄울 수 있는 조건인지 본다.
	//
	// 터미널에 붙어 있지 않으면(systemd, launchd, nohup, 파이프 등)
	// TUI가 /dev/tty를 못 열어 실패한다. 그런 경우는 서버로만 돈다.
	// 사용자가 --headless를 안 줬어도 알아서 판단해야 한다.
	attached := term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
	useTUI := !*headless && pty.Available() && attached

	// TUI가 뜨면 화면을 차지하므로 로그를 파일로 돌린다.
	// 안 그러면 로그가 화면 위에 찍혀 엉망이 된다.
	var logFile *os.File
	if useTUI {
		logFile = redirectLog(cfg)
		defer func() {
			if logFile != nil {
				_ = logFile.Close()
			}
		}()
	} else {
		banner(cfg, *headless)
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[terminal] 시작 실패: %v", err)
			stop()
		}
	}()

	// 클라이언트 모드: relay-service에 이쪽에서 붙는다.
	var link *relaylink.Link
	var model *tui.Model

	if cfg.RelayURL != "" {
		link = relaylink.New(relaylink.Options{
			URL:      cfg.RelayURL,
			Token:    cfg.RelayToken,
			Name:     cfg.RelayName,
			LocalURL: "http://127.0.0.1:" + strconv.Itoa(cfg.Port),
			APIKey:   cfg.APIKey,
			// 상태가 바뀌면 화면에 반영한다. 화면이 없으면 무시된다.
			OnChange: func(s relaylink.Status) {
				if model != nil {
					model.SetStatus(s)
				}
			},
		})
		go link.Run(ctx)
		log.Printf("[terminal] relay 접속: %s (이름: %s)", cfg.RelayURL, cfg.RelayName)
	}

	if !useTUI {
		switch {
		case *headless:
			// 사용자가 명시적으로 껐다. 아무 말 안 한다.
		case !pty.Available():
			log.Println("[terminal] PTY를 쓸 수 없어 서버로만 돕니다.")
		case !attached:
			log.Println("[terminal] 터미널에 붙어 있지 않아 서버로만 돕니다.")
		}
		<-ctx.Done()
		shutdown(srv, reg)
		return
	}

	// --- 콘솔 화면 ---

	dir, err := cfg.ResolveDir("")
	if err != nil {
		log.Printf("[terminal] 시작 디렉터리를 정할 수 없습니다: %v", err)
		fmt.Fprintf(os.Stderr, "시작 디렉터리를 정할 수 없습니다: %v\n", err)
		os.Exit(1)
	}

	var argv []string
	if cfg.Shell != "" {
		argv = []string{cfg.Shell}
	}

	// 시작할 때부터 올바른 크기를 준다.
	//
	// 안 주면 PTY 기본값(80x24)으로 뜬 뒤 첫 WindowSizeMsg가 와야 바뀐다.
	// 그 사이에 뜬 앱은 잘못된 크기를 믿고 그려서 화면이 한 줄씩 어긋난다.
	cols, rows := 80, 24
	if w, h, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 && h > 0 {
		cols = w
		// 상태바가 쓰는 만큼 뺀 높이를 준다.
		rows = h - tui.StatusHeight
		if rows < 1 {
			rows = 1
		}
	}

	local, err := reg.Create(terminal.CreateOptions{
		Dir: dir, Argv: argv, Cols: cols, Rows: rows,
	})
	if err != nil {
		log.Printf("[terminal] 셸을 띄우지 못했습니다: %v", err)
		fmt.Fprintf(os.Stderr, "셸을 띄우지 못했습니다: %v\n", err)
		os.Exit(1)
	}
	log.Printf("[terminal] 로컬 화면 시작 %s dir=%s", local.ID(), dir)

	model = tui.New(tui.Options{
		Terminal:     local,
		AgentName:    cfg.RelayName,
		RelayEnabled: cfg.RelayURL != "",
	})
	if link != nil {
		model.SetStatus(link.Status())
	}

	if err := tui.Run(ctx, model); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[terminal] 화면 오류: %v", err)
	}

	shutdown(srv, reg)
}

func shutdown(srv *http.Server, reg *terminal.Registry) {
	log.Println("[terminal] 종료합니다.")
	reg.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// redirectLog는 TUI가 화면을 차지하는 동안 로그를 파일로 보낸다.
// 파일을 못 열면 조용히 버린다 — 로그 때문에 화면이 깨지는 것보다 낫다.
func redirectLog(cfg config.Config) *os.File {
	path := cfg.LogPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.SetOutput(discard{})
		return nil
	}
	log.SetOutput(f)
	log.Printf("--- terminal-agent 시작 %s ---", time.Now().Format(time.RFC3339))
	return f
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func banner(cfg config.Config, headless bool) {
	fmt.Println()
	log.Printf("[terminal] http://%s:%d", cfg.Host, cfg.Port)
	log.Printf("[terminal] 허용 루트: %v", cfg.AllowedRoots)
	log.Printf("[terminal] 동시 터미널: %d, 유휴 정리: %v", cfg.MaxTerminals, cfg.IdleTimeout)
	if headless {
		log.Println("[terminal] --headless: 콘솔 화면 없이 서버로만 돕니다.")
	}

	if !pty.Available() {
		log.Println("[terminal] 경고: 이 시스템에서는 PTY를 쓸 수 없습니다 (윈도우는 10 1809 이상 필요)")
	}
	if cfg.APIKey == "" {
		log.Println("[terminal] 경고: TERMINAL_API_KEY가 없습니다. 인증 없이 열립니다.")
	}
	if !cfg.LocalOnly() {
		log.Printf("[terminal] 경고: %s 로 열려 있습니다. 셸을 여는 서비스이니 외부 노출을 피하세요.", cfg.Host)
	}
	fmt.Println()
}
