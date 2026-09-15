package terminal

import (
	"errors"
	"io/fs"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ANSI 이스케이프와 OSC를 걷어낸다. 프롬프트가 섞여도 내용만 보게 한다.
var ansi = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b\[[0-9;?]*[a-zA-Z]|\x1b[()][0-9A-Za-z]`)

func clean(s string) string {
	return strings.ReplaceAll(ansi.ReplaceAllString(s, ""), "\r", "")
}

// collect는 표식이 보일 때까지 출력을 모은다.
//
// PTY는 입력을 그대로 되울리므로(echo), 보낸 명령줄이 출력에 먼저 나온다.
// 그래서 표식이 명령 안에 들어 있으면 실행 결과가 아니라 에코에서 먼저
// 걸린다. count번째로 나타날 때까지 기다려 그 함정을 피한다.
func collect(t *testing.T, ch <-chan []byte, marker string, count int, wait time.Duration) string {
	t.Helper()

	var sb strings.Builder
	deadline := time.After(wait)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return sb.String()
			}
			sb.Write(b)
			if strings.Count(clean(sb.String()), marker) >= count {
				return sb.String()
			}
		case <-deadline:
			return sb.String()
		}
	}
}

func newTestTerminal(t *testing.T, o CreateOptions) (*Registry, *Terminal) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	if o.Cols == 0 {
		o.Cols, o.Rows = 80, 24
	}
	if len(o.Argv) == 0 {
		// 사용자 rc를 안 읽어야 출력이 깨끗하다.
		o.Argv = []string{"/bin/sh"}
	}

	r := NewRegistry(RegistryOptions{Max: 4, Scrollback: 64 * 1024})
	t.Cleanup(r.CloseAll)

	term, err := r.Create(o)
	if err != nil {
		// 컨테이너에는 /dev/ptmx가 없는 경우가 있다. PTY를 못 여는 곳에서는
		// 이 테스트가 성립하지 않으므로 건너뛴다. 다른 실패는 그대로 알린다.
		if errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "/dev/ptmx") {
			t.Skipf("PTY를 열 수 없는 환경이다: %v", err)
		}
		t.Fatalf("터미널 생성 실패: %v", err)
	}
	return r, term
}

// 셸이 실제로 TTY 위에서 도는지 본다. 파이프였다면 tty가 "not a tty"를 준다.
func TestTerminalIsRealTTY(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir()})

	ch, cancel := term.Subscribe(false)
	defer cancel()

	if err := term.Write([]byte("tty; echo D\"ONE\"-TTY\n")); err != nil {
		t.Fatal(err)
	}

	got := clean(collect(t, ch, "DONE-TTY", 1, 5*time.Second))
	if !strings.Contains(got, "/dev/") {
		t.Fatalf("TTY가 아니다. 출력:\n%s", got)
	}
}

// 우리가 준 크기를 셸이 그대로 보는지 본다.
func TestTerminalSize(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir(), Cols: 100, Rows: 40})

	ch, cancel := term.Subscribe(false)
	defer cancel()

	_ = term.Write([]byte("echo S\"IZE\"=$(tput cols)x$(tput lines)\n"))

	got := clean(collect(t, ch, "SIZE=", 1, 5*time.Second))
	if !strings.Contains(got, "SIZE=100x40") {
		t.Fatalf("크기가 안 맞는다. 출력:\n%s", got)
	}
}

// Resize 후 셸이 새 크기를 보는지 본다. SIGWINCH가 가야 한다.
func TestTerminalResize(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir(), Cols: 80, Rows: 24})

	ch, cancel := term.Subscribe(false)
	defer cancel()

	if err := term.Resize(120, 50); err != nil {
		t.Fatal(err)
	}
	_ = term.Write([]byte("echo A\"FTER\"=$(tput cols)x$(tput lines)\n"))

	got := clean(collect(t, ch, "AFTER=", 1, 5*time.Second))
	if !strings.Contains(got, "AFTER=120x50") {
		t.Fatalf("리사이즈가 안 먹었다. 출력:\n%s", got)
	}

	if info := term.Info(); info.Cols != 120 || info.Rows != 50 {
		t.Fatalf("Info가 갱신되지 않았다: %dx%d", info.Cols, info.Rows)
	}
}

// 한글이 깨지지 않는지 본다. UTF-8이 중간에 잘리면 여기서 드러난다.
func TestTerminalUTF8(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir()})

	ch, cancel := term.Subscribe(false)
	defer cancel()

	_ = term.Write([]byte("echo \"한글테스\"\"트-끝\"\n"))

	got := clean(collect(t, ch, "한글테스트-끝", 1, 5*time.Second))
	if !strings.Contains(got, "한글테스트-끝") {
		t.Fatalf("한글이 깨졌다. 출력:\n%q", got)
	}
}

// 시작 디렉터리가 실제로 적용되는지 본다.
func TestTerminalStartsInDir(t *testing.T) {
	dir := t.TempDir()
	_, term := newTestTerminal(t, CreateOptions{Dir: dir})

	ch, cancel := term.Subscribe(false)
	defer cancel()

	_ = term.Write([]byte("pwd; echo D\"ONE\"-PWD\n"))

	got := clean(collect(t, ch, "DONE-PWD", 1, 5*time.Second))
	// macOS에서 /var는 /private/var의 링크라 뒷부분만 확인한다.
	tail := dir[strings.LastIndex(dir, "/"):]
	if !strings.Contains(got, tail) {
		t.Fatalf("시작 위치가 다르다. want 끝=%s 출력:\n%s", tail, got)
	}
}

// 늦게 붙은 구독자가 이전 출력을 돌려받는지 본다.
func TestTerminalReplay(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir()})

	first, cancel1 := term.Subscribe(false)
	_ = term.Write([]byte("echo R\"EPLAY\"-MARK\n"))
	collect(t, first, "REPLAY-MARK", 1, 5*time.Second)
	cancel1()

	// 이제 새로 붙는다. 아까 출력이 보여야 한다.
	second, cancel2 := term.Subscribe(true)
	defer cancel2()

	select {
	case b := <-second:
		if !strings.Contains(clean(string(b)), "REPLAY-MARK") {
			t.Fatalf("재생 내용에 이전 출력이 없다: %q", clean(string(b)))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("재생이 오지 않았다")
	}
}

// 셸이 끝나면 상태와 종료 코드가 잡히는지 본다.
func TestTerminalExit(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir()})

	ch, cancel := term.Subscribe(false)
	defer cancel()

	_ = term.Write([]byte("exit 7\n"))

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("셸이 끝나지 않았다")
	}

	// 구독 채널도 닫혀야 한다.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				goto checked
			}
		case <-deadline:
			t.Fatal("구독 채널이 닫히지 않았다")
		}
	}

checked:
	info := term.Info()
	if info.Status != StatusExited {
		t.Fatalf("상태가 exited가 아니다: %s", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != 7 {
		t.Fatalf("종료 코드가 7이 아니다: %v", info.ExitCode)
	}
}

// 개수 제한이 걸리는지 본다.
func TestRegistryMax(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	r := NewRegistry(RegistryOptions{Max: 2, Scrollback: 1024})
	defer r.CloseAll()

	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, err := r.Create(CreateOptions{Dir: dir, Argv: []string{"/bin/sh"}}); err != nil {
			t.Fatalf("%d번째 생성 실패: %v", i, err)
		}
	}

	if _, err := r.Create(CreateOptions{Dir: dir, Argv: []string{"/bin/sh"}}); err != ErrTooMany {
		t.Fatalf("제한이 안 걸렸다: %v", err)
	}

	if got := len(r.List()); got != 2 {
		t.Fatalf("목록 개수가 다르다: %d", got)
	}
}

// 느린 구독자가 전체를 막지 않는지 본다.
// 채널을 안 읽는 구독자를 두고도 다른 구독자가 계속 받아야 한다.
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	_, term := newTestTerminal(t, CreateOptions{Dir: t.TempDir()})

	// 일부러 안 읽는다.
	_, cancelSlow := term.Subscribe(false)
	defer cancelSlow()

	fast, cancelFast := term.Subscribe(false)
	defer cancelFast()

	// 느린 쪽 버퍼를 넘길 만큼 출력을 만든다.
	_ = term.Write([]byte("for i in $(seq 1 200); do echo line-$i; done; echo F\"LOOD\"-DONE\n"))

	got := clean(collect(t, fast, "FLOOD-DONE", 1, 10*time.Second))
	if !strings.Contains(got, "FLOOD-DONE") {
		t.Fatal("느린 구독자 때문에 빠른 구독자가 막혔다")
	}
}
