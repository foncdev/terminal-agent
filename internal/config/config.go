// Package config는 환경변수에서 설정을 읽는다.
//
// .env 파일도 읽지만, 셸에 이미 있는 값이 우선한다.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host string
	Port int

	// APIKey가 비어 있으면 인증을 걸지 않는다. 로컬 전용일 때만 그렇게 쓴다.
	APIKey string

	// AllowedRoots는 터미널을 띄울 수 있는 디렉터리다.
	//
	// 주의: 이것은 시작 위치만 제한한다. 셸 안에서 cd로 나가는 것은
	// 막지 않는다(막을 방법도 마땅치 않다). 실수 방지용이지 보안 경계가 아니다.
	AllowedRoots []string

	// Shell이 비어 있으면 플랫폼 기본값을 쓴다.
	Shell string

	MaxTerminals int
	IdleTimeout  time.Duration

	// Scrollback은 터미널마다 메모리에 들고 있을 출력 바이트 수다.
	// 나중에 붙는 클라이언트에게 화면을 복원해주는 용도다.
	Scrollback int

	// --- 중계 서버 연결 (선택) ---

	// RelayURL이 비어 있으면 붙지 않고 자기 포트로만 연다.
	RelayURL   string
	RelayToken string
	// RelayName은 서버 목록에 보일 이름이다. 비우면 호스트명.
	RelayName string
}

func Load() Config {
	loadDotEnv(".env")

	return Config{
		Host:         str("TERMINAL_HOST", "127.0.0.1"),
		Port:         num("TERMINAL_PORT", 4200),
		APIKey:       str("TERMINAL_API_KEY", ""),
		AllowedRoots: roots(str("TERMINAL_ALLOWED_ROOTS", defaultRoot())),
		Shell:        str("TERMINAL_SHELL", ""),
		MaxTerminals: num("TERMINAL_MAX", 3),
		IdleTimeout:  time.Duration(num("TERMINAL_IDLE_MINUTES", 30)) * time.Minute,
		Scrollback:   num("TERMINAL_SCROLLBACK_BYTES", 256*1024),

		RelayURL:   str("RELAY_URL", ""),
		RelayToken: str("RELAY_TERMINAL_TOKEN", ""),
		RelayName:  str("RELAY_AGENT_NAME", hostname()),
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "terminal-agent"
	}
	return h
}

// LocalOnly는 루프백에만 열려 있는지 본다.
// 밖으로 열려 있는데 키가 없으면 경고해야 한다.
func (c Config) LocalOnly() bool {
	return c.Host == "127.0.0.1" || c.Host == "localhost" || c.Host == "::1"
}

// LogPath는 콘솔 화면이 떠 있는 동안 로그를 적을 곳이다.
// 화면을 차지한 상태에서 stdout에 찍으면 화면이 깨진다.
func (c Config) LogPath() string {
	if p := os.Getenv("TERMINAL_LOG"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "terminal-agent.log")
}

func defaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

// roots는 구분자로 나눈다. 윈도우는 경로에 콜론(C:\)이 있어 세미콜론을 쓴다.
func roots(raw string) []string {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}

	var out []string
	for _, p := range strings.Split(raw, sep) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, expandHome(p))
	}
	return out
}

func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~"+string(filepath.Separator)) && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}

func str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func num(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// loadDotEnv는 KEY=VALUE 줄을 읽어 환경에 넣는다.
// 이미 설정된 값은 건드리지 않는다.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
