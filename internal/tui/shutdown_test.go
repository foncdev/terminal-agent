package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	creack "github.com/creack/pty"
)

// 종료 경로가 여럿이다. 전부 셸까지 정리되는지 확인한다.
//
// 셸이 남으면 좀비 프로세스가 쌓이고, 다음 실행에서 개수 제한에 걸린다.
func TestShutdownPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	bin := buildAgent(t)

	tests := []struct {
		name string
		// stop은 종료를 일으킨다. f는 TUI의 가짜 터미널.
		stop func(t *testing.T, f *os.File, cmd *exec.Cmd)
	}{
		{
			name: "ctrl+백슬래시",
			stop: func(t *testing.T, f *os.File, _ *exec.Cmd) {
				_, _ = f.Write([]byte{0x1c})
			},
		},
		{
			name: "셸에서 exit",
			stop: func(t *testing.T, f *os.File, _ *exec.Cmd) {
				_, _ = f.WriteString("exit\r")
			},
		},
		{
			name: "SIGTERM (kill)",
			stop: func(t *testing.T, _ *os.File, cmd *exec.Cmd) {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			},
		},
		{
			name: "SIGINT (kill -INT)",
			stop: func(t *testing.T, _ *os.File, cmd *exec.Cmd) {
				_ = cmd.Process.Signal(syscall.SIGINT)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, cmd, emu := startAgent(t, bin)
			waitScreen(t, emu, "로컬 전용", 8*time.Second)

			// 셸이 실제로 떴는지 확인한다. 그래야 "정리됐다"가 의미 있다.
			_, _ = f.WriteString("echo READY\r")
			waitScreen(t, emu, "READY", 5*time.Second)

			shellPID := findShellPID(t, emu, f)

			tc.stop(t, f, cmd)

			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()

			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Logf("화면:\n%s", dump(screenLines(emu), 5))
				t.Fatalf("프로세스가 끝나지 않았다 (pid=%d shell=%d)", cmd.Process.Pid, shellPID)
			}

			// 셸도 같이 죽어야 한다.
			if shellPID > 0 && processAlive(shellPID) {
				// 조금 기다려본다. 정리에 시간이 걸릴 수 있다.
				time.Sleep(time.Second)
				if processAlive(shellPID) {
					t.Fatalf("셸(pid %d)이 살아남았다", shellPID)
				}
			}
		})
	}
}

// startAgent는 가짜 터미널에서 바이너리를 띄우고 화면을 읽을 준비를 한다.
func startAgent(t *testing.T, bin string) (*os.File, *exec.Cmd, *vt.SafeEmulator) {
	t.Helper()

	const cols, rows = 90, 24
	dir := t.TempDir()

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"TERMINAL_ENABLED=true",
		"TERMINAL_PORT=0",
		"TERMINAL_ALLOWED_ROOTS="+dir,
		"TERMINAL_SHELL=/bin/sh",
		"TERMINAL_LOG="+filepath.Join(dir, "agent.log"),
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

	emu := vt.NewSafeEmulator(cols, rows)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := emu.Read(buf); err != nil {
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	return f, cmd, emu
}

// findShellPID는 셸에게 자기 pid를 물어본다.
func findShellPID(t *testing.T, emu *vt.SafeEmulator, f *os.File) int {
	t.Helper()

	_, _ = f.WriteString("echo P\"ID\"=$$\r")
	lines := waitScreen(t, emu, "PID=", 5*time.Second)

	for _, l := range lines {
		i := strings.Index(l, "PID=")
		if i < 0 {
			continue
		}
		var pid int
		if _, err := fmtSscan(l[i+4:], &pid); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// fmtSscan은 문자열 앞쪽의 숫자만 읽는다.
func fmtSscan(s string, out *int) (int, error) {
	n := 0
	read := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		read++
	}
	if read == 0 {
		return 0, errNoNumber
	}
	*out = n
	return read, nil
}

var errNoNumber = &parseError{"숫자가 없다"}

type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }

// processAlive는 프로세스가 아직 있는지 본다.
// 시그널 0은 실제로 보내지 않고 존재만 확인한다.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
