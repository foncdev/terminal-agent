package tui

import (
	tea "charm.land/bubbletea/v2"
)

// keyToBytes는 Bubble Tea 키 이벤트를 터미널이 이해하는 바이트로 바꾼다.
//
// 우리가 화면을 대신 그리고 있으므로, 키도 대신 번역해 셸에 넣어야 한다.
// 실제 터미널이 하던 일을 우리가 하는 셈이다.
//
// 이스케이프 시퀀스는 xterm 관례를 따른다. 자식 프로그램(vim, top 등)이
// 그걸 기대하기 때문이다.
func keyToBytes(k tea.KeyPressMsg) []byte {
	key := k.Key()

	// 수식키가 붙은 특수키를 먼저 본다.
	// xterm은 CSI 1;<mod><letter> 꼴로 보낸다. mod는 1 + 비트합이다.
	if mod := xtermMod(key.Mod); mod > 1 {
		if seq := modifiedSpecial(k.String(), mod); seq != nil {
			return seq
		}
	}

	switch k.String() {
	case "enter":
		return []byte("\r")
	case "backspace":
		// 대부분의 유닉스 터미널이 DEL을 보낸다. BS(0x08)가 아니다.
		return []byte{0x7f}
	case "tab":
		return []byte("\t")
	case "shift+tab":
		return []byte("\x1b[Z")
	case "esc":
		return []byte{0x1b}
	case "space":
		return []byte(" ")

	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")

	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")
	case "insert":
		return []byte("\x1b[2~")
	case "delete":
		return []byte("\x1b[3~")

	case "f1":
		return []byte("\x1bOP")
	case "f2":
		return []byte("\x1bOQ")
	case "f3":
		return []byte("\x1bOR")
	case "f4":
		return []byte("\x1bOS")
	case "f5":
		return []byte("\x1b[15~")
	case "f6":
		return []byte("\x1b[17~")
	case "f7":
		return []byte("\x1b[18~")
	case "f8":
		return []byte("\x1b[19~")
	case "f9":
		return []byte("\x1b[20~")
	case "f10":
		return []byte("\x1b[21~")
	case "f11":
		return []byte("\x1b[23~")
	case "f12":
		return []byte("\x1b[24~")
	}

	// Ctrl 조합은 제어문자가 된다. ctrl+a=0x01 … ctrl+z=0x1a.
	if key.Mod&tea.ModCtrl != 0 {
		switch c := key.Code; {
		case c >= 'a' && c <= 'z':
			return []byte{byte(c - 'a' + 1)}
		case c >= 'A' && c <= 'Z':
			return []byte{byte(c - 'A' + 1)}
		case c == ' ':
			return []byte{0} // ctrl+space = NUL
		case c == '[':
			return []byte{0x1b}
		case c == '\\':
			return []byte{0x1c}
		case c == ']':
			return []byte{0x1d}
		case c == '^':
			return []byte{0x1e}
		case c == '_':
			return []byte{0x1f}
		}
	}

	// Alt는 앞에 ESC를 붙이는 관례(meta prefix)를 쓴다.
	if key.Mod&tea.ModAlt != 0 && key.Text != "" {
		return append([]byte{0x1b}, []byte(key.Text)...)
	}

	if key.Text != "" {
		return []byte(key.Text)
	}
	return nil
}

// xtermMod는 수식키를 xterm의 숫자로 바꾼다.
// 1을 더한 값을 쓴다: shift=2, alt=3, shift+alt=4, ctrl=5 …
func xtermMod(m tea.KeyMod) int {
	n := 1
	if m&tea.ModShift != 0 {
		n += 1
	}
	if m&tea.ModAlt != 0 {
		n += 2
	}
	if m&tea.ModCtrl != 0 {
		n += 4
	}
	return n
}

// modifiedSpecial은 수식키가 붙은 방향키·Home/End를 만든다.
// 예: ctrl+right = "\x1b[1;5C" (단어 단위 이동)
func modifiedSpecial(name string, mod int) []byte {
	var final byte
	switch {
	case hasSuffix(name, "up"):
		final = 'A'
	case hasSuffix(name, "down"):
		final = 'B'
	case hasSuffix(name, "right"):
		final = 'C'
	case hasSuffix(name, "left"):
		final = 'D'
	case hasSuffix(name, "end"):
		final = 'F'
	case hasSuffix(name, "home"):
		final = 'H'
	default:
		return nil
	}

	return []byte{0x1b, '[', '1', ';', byte('0' + mod), final}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
