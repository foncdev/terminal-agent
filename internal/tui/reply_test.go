package tui

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"

	"github.com/foncdev/terminal-agent/internal/terminal"
)

// 에뮬레이터는 터미널 질의에 답을 만든다.
//
// 그 답을 PTY에 써넣으면 셸이 키 입력으로 받아버린다. 실제로 겪은
// 버그라 응답이 존재한다는 사실을 기록해둔다.
func TestEmulatorProducesReply(t *testing.T) {
	emu := vt.NewSafeEmulator(80, 24)

	got := make(chan []byte, 4)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := emu.Read(buf)
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				got <- b
			}
			if err != nil {
				return
			}
		}
	}()

	// DA1: "어떤 터미널이냐"
	_, _ = emu.Write([]byte("\x1b[c"))

	select {
	case b := <-got:
		s := string(b)
		if !strings.HasPrefix(s, "\x1b[") || !strings.HasSuffix(s, "c") {
			t.Fatalf("DA1 응답 모양이 다르다: %q", s)
		}
		t.Logf("에뮬레이터 응답(버려야 할 것): %q", s)
	case <-time.After(3 * time.Second):
		t.Fatal("응답이 없다 — pumpReplies의 전제가 바뀌었는지 확인하라")
	}
}

// 응답을 PTY에 써넣으면 실제로 무슨 일이 나는지 못 박아둔다.
//
// 이 테스트는 "버그를 재현하면 이렇게 된다"를 보여준다.
// pumpReplies가 다시 term.Write를 하게 되면 사용자가 보는 화면이 이 꼴이 된다.
func TestReplyWrittenToPtyBecomesInput(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	term := newShell(t)
	out, unsub := term.Subscribe(false)
	defer unsub()
	time.Sleep(500 * time.Millisecond)

	// 에뮬레이터가 내놓는 것과 같은 응답을 PTY에 써본다.
	_ = term.Write([]byte("\x1b[?62;1;6;22c"))
	time.Sleep(300 * time.Millisecond)
	_ = term.Write([]byte("\n"))

	got := drain(out, 3*time.Second)

	// 셸은 이걸 명령으로 받아 실행하려 든다.
	if !strings.Contains(got, "not found") {
		t.Skipf("이 셸은 다르게 반응한다(%q). 아래 테스트가 본체다.", tail(got, 120))
	}
	t.Logf("응답이 입력으로 새면 이렇게 된다: %s", tail(strings.TrimSpace(got), 160))
}

// pumpReplies가 응답을 셸에 되돌리지 않는지 본다.
//
// Model을 실제로 띄운 상태에서, 에뮬레이터에 질의를 직접 먹여
// 응답을 만들게 한다. 그 응답이 PTY로 새면 셸이 명령으로 받아
// "not found"를 뱉는다.
func TestPumpRepliesDoesNotWriteBack(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	term := newShell(t)

	m := New(Options{Terminal: term, AgentName: "test"})
	defer m.Close()
	m.Init() // 구독 시작. New가 pumpReplies를 띄운다.

	out, unsub := term.Subscribe(false)
	defer unsub()
	time.Sleep(500 * time.Millisecond)

	// 자식이 질의를 보낸 것과 같은 상황을 만든다.
	// 에뮬레이터가 답을 만들고, pumpReplies가 그걸 처리한다.
	for i := 0; i < 3; i++ {
		_, _ = m.emu.Write([]byte("\x1b[c"))
	}
	time.Sleep(1500 * time.Millisecond)

	// 프롬프트를 한 번 돌린다. 샜다면 여기서 드러난다.
	_ = term.Write([]byte("echo M\"ARK\"\n"))

	got := drain(out, 4*time.Second)

	if strings.Contains(got, "not found") {
		t.Fatalf("응답이 셸 입력으로 샜다:\n%s", tail(got, 400))
	}
	if !strings.Contains(got, "MARK") {
		t.Fatalf("셸이 정상 동작하지 않는다:\n%s", tail(got, 400))
	}
}

// --- 헬퍼 ---

func newShell(t *testing.T) *terminal.Terminal {
	t.Helper()

	reg := terminal.NewRegistry(terminal.RegistryOptions{Max: 2, Scrollback: 64 * 1024})
	t.Cleanup(reg.CloseAll)

	term, err := reg.Create(terminal.CreateOptions{
		Dir:  t.TempDir(),
		Argv: []string{"/bin/sh"},
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	return term
}

func drain(ch <-chan []byte, wait time.Duration) string {
	var sb strings.Builder
	deadline := time.After(wait)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return sb.String()
			}
			sb.Write(b)
		case <-deadline:
			return sb.String()
		}
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
