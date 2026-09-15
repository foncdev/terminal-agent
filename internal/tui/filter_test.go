package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/foncdev/terminal-agent/internal/terminal"
)

// 실제 증상에서 잡힌 바이트들을 그대로 넣어 걸러지는지 본다.
//
// claude를 쓰고 /exit로 나오면 5초마다 이런 것이 들어왔고,
// 그대로 셸에 전달돼 "1;2c1;2c1;2c…"가 화면에 쌓였다.
func TestIsTerminalReply(t *testing.T) {
	block := []struct {
		name string
		in   string
	}{
		// 추적 파일에서 실제로 잡힌 것들
		{"포커스 아웃", "\x1b[O"},
		{"포커스 인", "\x1b[I"},
		{"DA1 응답 (iTerm)", "\x1b[?1;2c"},
		{"DA1 응답 (x/vt)", "\x1b[?62;1;6;22c"},
		{"DA2 응답", "\x1b[>0;95;0c"},
		{"커서 위치 응답", "\x1b[24;80R"},
		{"모드 상태 응답", "\x1b[?1004;1$y"},
		{"창 크기 응답", "\x1b[8;24;80t"},
		{"배경색 응답", "\x1b]11;rgb:1616/1616/1a1a\x07"},

		// 실제로는 여러 개가 한 덩어리로 온다. 추적에서 그대로 가져왔다.
		{"붙어 온 것 2개", "\x1b[O\x1b[?1;2c"},
		{"붙어 온 것 4개", "\x1b[O\x1b[?1;2c\x1b[O\x1b[?1;2c"},
		{"DA만 연속", "\x1b[?1;2c\x1b[?1;2c\x1b[?1;2c"},
	}

	for _, tc := range block {
		t.Run("막아야: "+tc.name, func(t *testing.T) {
			if !terminal.IsTerminalReply([]byte(tc.in)) {
				t.Fatalf("걸러내지 못했다: %q", tc.in)
			}
		})
	}

	// 진짜 키 입력은 통과해야 한다. 과하게 막으면 터미널을 못 쓴다.
	pass := []struct {
		name string
		in   string
	}{
		{"글자", "a"},
		{"한글", "한"},
		{"엔터", "\r"},
		{"백스페이스", "\x7f"},
		{"ESC 단독", "\x1b"},
		{"위 화살표", "\x1b[A"},
		{"아래 화살표", "\x1b[B"},
		{"오른쪽", "\x1b[C"},
		{"왼쪽", "\x1b[D"},
		{"Home", "\x1b[H"},
		{"End", "\x1b[F"},
		{"Delete", "\x1b[3~"},
		{"F5", "\x1b[15~"},
		{"ctrl+right", "\x1b[1;5C"},
		{"shift+tab", "\x1b[Z"},
		{"ctrl+c", "\x03"},
		{"alt+b", "\x1bb"},

		// 응답과 진짜 입력이 섞이면 막지 않는다.
		// 사용자가 친 키를 잃는 쪽이 더 나쁘다.
		{"응답+글자", "\x1b[?1;2ca"},
		{"글자+응답", "a\x1b[?1;2c"},
	}

	for _, tc := range pass {
		t.Run("통과해야: "+tc.name, func(t *testing.T) {
			if terminal.IsTerminalReply([]byte(tc.in)) {
				t.Fatalf("키 입력을 막아버렸다: %q", tc.in)
			}
		})
	}
}

// 화살표(CSI A~D)와 DA 응답(CSI ? … c)이 헷갈리지 않아야 한다.
// 둘 다 CSI로 시작해서 구분이 까다롭다.
func TestFilterDoesNotBreakArrowKeys(t *testing.T) {
	for _, k := range []string{"up", "down", "left", "right", "home", "end"} {
		var msg = pressByName(k)
		data := keyToBytes(msg)
		if len(data) == 0 {
			t.Fatalf("%s: 변환 결과가 없다", k)
		}
		if terminal.IsTerminalReply(data) {
			t.Fatalf("%s(%q)를 응답으로 잘못 봤다", k, data)
		}
	}
}

func pressByName(name string) tea.KeyPressMsg {
	codes := map[string]rune{
		"up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd,
	}
	return tea.KeyPressMsg{Code: codes[name]}
}
