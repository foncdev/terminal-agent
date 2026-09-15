package terminal

import "testing"

// 실제 증상에서 잡힌 것들을 그대로 넣는다.
//
// 두 번 겪었다:
//   - claude 후: "\x1b[O\x1b[?1;2c" 가 반복돼 "1;2c1;2c…"
//   - vi 실행 시: OSC 색상 응답이 새어 "rgb:1616/1616/1a1a"
func TestIsTerminalReplyBlocks(t *testing.T) {
	cases := []struct{ name, in string }{
		{"포커스 아웃", "\x1b[O"},
		{"포커스 인", "\x1b[I"},
		{"DA1 (iTerm)", "\x1b[?1;2c"},
		{"DA1 (x/vt)", "\x1b[?62;1;6;22c"},
		{"DA2", "\x1b[>0;95;0c"},
		{"커서 위치(CPR)", "\x1b[24;80R"},
		{"모드 상태(DECRPM)", "\x1b[?1004;1$y"},
		{"창 크기", "\x1b[8;24;80t"},

		// 붙어서 오는 경우 — 실제로 이렇게 왔다
		{"포커스+DA 반복", "\x1b[O\x1b[?1;2c\x1b[O\x1b[?1;2c"},
		{"DA 연속", "\x1b[?1;2c\x1b[?1;2c\x1b[?1;2c"},

		// OSC 색상 응답. 종결자가 BEL과 ST 두 가지다.
		{"OSC 배경색 (BEL)", "\x1b]11;rgb:1616/1616/1a1a\x07"},
		{"OSC 배경색 (ST)", "\x1b]11;rgb:1616/1616/1a1a\x1b\\"},
		{"OSC 전경색 (BEL)", "\x1b]10;rgb:e6e6/e6e6/eaea\x07"},
		{"OSC 전경색 (ST)", "\x1b]10;rgb:e6e6/e6e6/eaea\x1b\\"},
		{"DA + OSC 둘", "\x1b[?1;2c\x1b]11;rgb:1616/1616/1a1a\x07\x1b]10;rgb:e6e6/e6e6/eaea\x07"},
		{"CPR + OSC", "\x1b[2;76R\x1b]11;rgb:1616/1616/1a1a\x07"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsTerminalReply([]byte(tc.in)) {
				t.Fatalf("막지 못했다: %q", tc.in)
			}
		})
	}
}

// 진짜 입력은 통과해야 한다. 과하게 막으면 터미널을 못 쓴다.
func TestIsTerminalReplyPasses(t *testing.T) {
	cases := []struct{ name, in string }{
		{"글자", "a"},
		{"한글", "한"},
		{"엔터", "\r"},
		{"탭", "\t"},
		{"백스페이스", "\x7f"},
		{"ESC 단독", "\x1b"},
		{"위", "\x1b[A"},
		{"아래", "\x1b[B"},
		{"오른쪽", "\x1b[C"},
		{"왼쪽", "\x1b[D"},
		{"Home", "\x1b[H"},
		{"End", "\x1b[F"},
		{"Delete", "\x1b[3~"},
		{"PgUp", "\x1b[5~"},
		{"F1", "\x1bOP"},
		{"F5", "\x1b[15~"},
		{"ctrl+right", "\x1b[1;5C"},
		{"shift+tab", "\x1b[Z"},
		{"ctrl+c", "\x03"},
		{"ctrl+d", "\x04"},
		{"alt+b", "\x1bb"},

		// 응답과 입력이 섞이면 막지 않는다.
		// 사용자가 친 키를 잃는 쪽이 더 나쁘다.
		{"응답+글자", "\x1b[?1;2ca"},
		{"글자+응답", "a\x1b[?1;2c"},
		{"OSC+글자", "\x1b]11;rgb:1616/1616/1a1a\x07x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsTerminalReply([]byte(tc.in)) {
				t.Fatalf("입력을 막아버렸다: %q", tc.in)
			}
		})
	}
}

// seqLen이 시퀀스 경계를 제대로 잡는지.
// 여기가 틀리면 조각이 남아 화면에 글자로 찍힌다.
func TestSeqLen(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"\x1b[A", 3},                            // CSI 짧은 것
		{"\x1b[1;5C", 6},                         // CSI 파라미터
		{"\x1b[?1;2c", 7},                        // CSI 프라이빗
		{"\x1b]11;rgb:aaaa/bbbb/cccc\x07", 24},   // OSC + BEL
		{"\x1b]11;rgb:aaaa/bbbb/cccc\x1b\\", 25}, // OSC + ST
		{"\x1bb", 2},                             // ESC + 글자
		{"\x1b", 1},                              // ESC 단독
	}
	for _, tc := range cases {
		if got := seqLen(tc.in); got != tc.want {
			t.Fatalf("%q: got=%d want=%d", tc.in, got, tc.want)
		}
	}
}
