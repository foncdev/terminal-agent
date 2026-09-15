package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	creack "github.com/creack/pty"
)

// TUI는 사람이 봐야 아는 물건이라, 가짜 터미널 안에서 돌리고
// 그 화면을 에뮬레이터로 해석해 검사한다.
//
// 빌드가 필요해 시간이 걸리므로 -short에서는 건너뛴다.
func TestLocalConsole(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	bin := buildAgent(t)

	const cols, rows = 100, 30

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"TERMINAL_ENABLED=true",
		"TERMINAL_PORT=0", // 포트 충돌을 피한다
		"TERMINAL_ALLOWED_ROOTS="+t.TempDir(),
		"TERMINAL_SHELL=/bin/sh",
		"TERMINAL_LOG="+filepath.Join(t.TempDir(), "agent.log"),
	)

	f, err := creack.StartWithSize(cmd, &creack.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	// TUI가 그리는 화면을 해석할 바깥쪽 에뮬레이터.
	outer := vt.NewSafeEmulator(cols, rows)

	// 에뮬레이터 응답을 반드시 비워야 한다. 안 그러면 내부 파이프가
	// 막히면서 뮤텍스를 쥔 채 데드락에 빠진다.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := outer.Read(buf); err != nil {
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				_, _ = outer.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	waitScreen(t, outer, "로컬 전용", 8*time.Second)

	t.Run("상태바가 1행에 고정", func(t *testing.T) {
		line := screenLines(outer)[0]
		if !strings.Contains(line, "로컬 전용") {
			t.Fatalf("1행에 상태바가 없다: %q", line)
		}
		if !strings.Contains(line, "나가기") {
			t.Fatalf("나가기 안내가 없다: %q", line)
		}
	})

	t.Run("셸이 명령을 실행", func(t *testing.T) {
		_, _ = f.WriteString("echo TUI-OK\r")
		lines := waitScreen(t, outer, "TUI-OK", 5*time.Second)
		if !contains(lines, "TUI-OK") {
			t.Fatalf("명령 결과가 없다:\n%s", dump(lines, 6))
		}
		if !strings.Contains(lines[0], "로컬 전용") {
			t.Fatalf("상태바가 사라졌다: %q", lines[0])
		}
	})

	t.Run("셸이 상태바만큼 줄어든 높이를 봄", func(t *testing.T) {
		_, _ = f.WriteString("echo SZ=$(tput cols)x$(tput lines)\r")
		want := fmt.Sprintf("SZ=%dx%d", cols, rows-statusHeight)
		lines := waitScreen(t, outer, want, 5*time.Second)
		if !contains(lines, want) {
			t.Fatalf("%s 를 못 찾았다:\n%s", want, dump(lines, 8))
		}
	})

	t.Run("한글", func(t *testing.T) {
		_, _ = f.WriteString("echo 본구현한글\r")
		lines := waitScreen(t, outer, "본구현한글", 5*time.Second)
		if !contains(lines, "본구현한글") {
			t.Fatalf("한글이 깨졌다:\n%s", dump(lines, 8))
		}
	})

	// 핵심: 자식이 전체화면에 들어가 1행에 그려도 상태바가 살아야 한다.
	t.Run("전체화면 앱이 상태바를 덮지 못함", func(t *testing.T) {
		_, _ = f.WriteString("printf '\\033[?1049h\\033[HFULLSCREEN'\r")
		lines := waitScreen(t, outer, "FULLSCREEN", 5*time.Second)
		if !contains(lines, "FULLSCREEN") {
			t.Fatalf("전체화면 내용이 안 보인다:\n%s", dump(lines, 6))
		}
		if !strings.Contains(lines[0], "로컬 전용") {
			t.Fatalf("자식이 상태바를 덮었다: %q", lines[0])
		}
		// 원래 화면으로 돌아간다.
		_, _ = f.WriteString("printf '\\033[?1049l'\r")
	})

	t.Run("ctrl+백슬래시로 종료", func(t *testing.T) {
		_, _ = f.Write([]byte{0x1c}) // ctrl+\

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("종료되지 않았다")
		}
	})
}

// buildAgent는 테스트용 바이너리를 만든다.
func buildAgent(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "terminal-agent-test")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/terminal-agent")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("빌드 실패: %v\n%s", err, out)
	}
	return bin
}

func screenLines(e *vt.SafeEmulator) []string {
	out := make([]string, 0, e.Height())
	for y := 0; y < e.Height(); y++ {
		var sb strings.Builder
		for x := 0; x < e.Width(); x++ {
			if c := e.CellAt(x, y); c != nil {
				sb.WriteString(c.String())
			}
		}
		out = append(out, strings.TrimRight(sb.String(), " "))
	}
	return out
}

func waitScreen(t *testing.T, e *vt.SafeEmulator, want string, wait time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		lines := screenLines(e)
		if contains(lines, want) {
			return lines
		}
		time.Sleep(80 * time.Millisecond)
	}
	return screenLines(e)
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

func dump(lines []string, n int) string {
	var sb strings.Builder
	for i, l := range lines {
		if i >= n {
			break
		}
		fmt.Fprintf(&sb, "  %2d│%s\n", i, l)
	}
	return sb.String()
}
