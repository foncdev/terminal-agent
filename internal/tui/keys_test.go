package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// 키 변환이 틀리면 셸이 엉뚱하게 동작한다. 자주 쓰는 것부터 확인한다.
func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"엔터는 CR", press(tea.KeyEnter, 0), "\r"},
		{"백스페이스는 DEL", press(tea.KeyBackspace, 0), "\x7f"},
		{"탭", press(tea.KeyTab, 0), "\t"},
		{"ESC", press(tea.KeyEscape, 0), "\x1b"},

		{"위", press(tea.KeyUp, 0), "\x1b[A"},
		{"아래", press(tea.KeyDown, 0), "\x1b[B"},
		{"오른쪽", press(tea.KeyRight, 0), "\x1b[C"},
		{"왼쪽", press(tea.KeyLeft, 0), "\x1b[D"},

		{"Home", press(tea.KeyHome, 0), "\x1b[H"},
		{"End", press(tea.KeyEnd, 0), "\x1b[F"},
		{"PgUp", press(tea.KeyPgUp, 0), "\x1b[5~"},
		{"Delete", press(tea.KeyDelete, 0), "\x1b[3~"},

		{"F1", press(tea.KeyF1, 0), "\x1bOP"},
		{"F5", press(tea.KeyF5, 0), "\x1b[15~"},
		{"F12", press(tea.KeyF12, 0), "\x1b[24~"},

		// 셸에서 제일 많이 쓰는 조합들.
		{"ctrl+c는 SIGINT", text("c", tea.ModCtrl), "\x03"},
		{"ctrl+d는 EOF", text("d", tea.ModCtrl), "\x04"},
		{"ctrl+z는 SIGTSTP", text("z", tea.ModCtrl), "\x1a"},
		{"ctrl+a는 줄 처음", text("a", tea.ModCtrl), "\x01"},

		// 단어 단위 이동. 이게 없으면 편집이 불편하다.
		{"ctrl+right", press(tea.KeyRight, tea.ModCtrl), "\x1b[1;5C"},
		{"ctrl+left", press(tea.KeyLeft, tea.ModCtrl), "\x1b[1;5D"},
		{"shift+right", press(tea.KeyRight, tea.ModShift), "\x1b[1;2C"},

		{"shift+tab", press(tea.KeyTab, tea.ModShift), "\x1b[Z"},
		{"일반 글자", text("a", 0), "a"},
		{"스페이스", press(tea.KeySpace, 0), " "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(keyToBytes(tc.key))
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

// 한글은 Text로 들어온다. 그대로 나가야 한다.
func TestKeyToBytesUTF8(t *testing.T) {
	k := text("한", 0)
	if got := string(keyToBytes(k)); got != "한" {
		t.Fatalf("한글이 깨졌다: %q", got)
	}
}

// Alt는 앞에 ESC를 붙이는 관례를 쓴다.
func TestKeyToBytesAlt(t *testing.T) {
	k := text("b", tea.ModAlt)
	if got := string(keyToBytes(k)); got != "\x1bb" {
		t.Fatalf("got=%q want=%q", got, "\x1bb")
	}
}

func TestXtermMod(t *testing.T) {
	tests := []struct {
		mod  tea.KeyMod
		want int
	}{
		{0, 1},
		{tea.ModShift, 2},
		{tea.ModAlt, 3},
		{tea.ModShift | tea.ModAlt, 4},
		{tea.ModCtrl, 5},
		{tea.ModShift | tea.ModCtrl, 6},
	}
	for _, tc := range tests {
		if got := xtermMod(tc.mod); got != tc.want {
			t.Fatalf("mod=%v got=%d want=%d", tc.mod, got, tc.want)
		}
	}
}

// --- 헬퍼 ---

func press(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func text(s string, mod tea.KeyMod) tea.KeyPressMsg {
	r := []rune(s)[0]
	k := tea.KeyPressMsg{Code: r, Mod: mod}
	// Ctrl 조합에는 Text가 비어 있는 게 보통이다.
	if mod&tea.ModCtrl == 0 {
		k.Text = s
	}
	return k
}
