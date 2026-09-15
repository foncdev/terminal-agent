// 터미널이 보내온 "응답"을 걸러낸다.
//
// 바깥 터미널은 포커스 변화나 장치 질의에 스스로 답을 보낸다. 그 답은
// 우리 프로그램에게 온 것이지 셸에게 온 것이 아니다. 그대로 흘려보내면
// 셸이 키 입력으로 받아 "1;2c" 같은 글자를 찍는다.
//
// 맥 콘솔과 웹 어느 쪽으로 들어오든 같은 문제라 여기 둔다.
package terminal

import "strings"

// isTerminalReply는 "터미널이 우리에게 보낸 답"인지 본다.
//
// 바깥 터미널(iTerm 등)은 포커스 변화나 장치 질의에 스스로 응답을 보낸다.
// 그 응답은 우리 프로그램에게 온 것이지 셸에게 온 것이 아니다. 그대로
// 흘려보내면 셸이 키 입력으로 받아 "1;2c" 같은 글자를 찍는다.
//
// 실제로 claude를 쓰고 나오면 이런 것이 5초마다 들어왔다:
//
//	"\x1b[O"          포커스 잃음
//	"\x1b[?1;2c"      DA1 응답
//
// Bubble Tea가 대부분 걸러주지만, 모드가 어긋난 상태에서는 원시 바이트가
// 키로 들어온다. 여기서 한 번 더 막는다.
// 여러 개가 한 덩어리로 오는 경우가 많다. 실제로 이렇게 들어왔다:
//
//	"\x1b[O\x1b[?1;2c\x1b[O\x1b[?1;2c"
//
// 그래서 통짜로 보지 말고 시퀀스 단위로 쪼개 전부 응답인지 본다.
// 하나라도 진짜 입력이 섞여 있으면 막지 않는다 — 사용자가 친 키를
// 잃는 쪽이 글자가 좀 새는 것보다 나쁘다.
func IsTerminalReply(b []byte) bool {
	parts := splitEscapes(b)
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !isSingleReply(p) {
			return false
		}
	}
	return true
}

// splitEscapes는 ESC로 시작하는 시퀀스 단위로 자른다.
//
// OSC(ESC ])는 종결자가 BEL(\a)이거나 ST(ESC \)다. ST는 그 자체에
// ESC가 들어 있어서, 단순히 "다음 ESC까지"로 자르면 시퀀스가 두 동강
// 난다. 그러면 뒤 조각이 입력으로 새어 "rgb:1616/..." 같은 글자가 찍힌다.
// 실제로 vi를 띄웠을 때 그렇게 됐다.
func splitEscapes(b []byte) []string {
	s := string(b)
	if s == "" {
		return nil
	}

	var out []string
	for len(s) > 0 {
		if s[0] != 0x1b {
			// 평범한 글자 구간. 다음 ESC까지 한 덩어리.
			i := strings.IndexByte(s, 0x1b)
			if i < 0 {
				out = append(out, s)
				break
			}
			out = append(out, s[:i])
			s = s[i:]
			continue
		}

		n := seqLen(s)
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}

// seqLen은 s 앞부분의 이스케이프 시퀀스 길이를 잰다.
// 최소 1을 반환해 무한 루프를 막는다.
func seqLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}

	switch s[1] {
	case ']', 'P', 'X', '^', '_':
		// 문자열류(OSC/DCS/SOS/PM/APC): BEL 또는 ST(ESC \)로 끝난다.
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s) // 아직 안 끝났다

	case '[':
		// CSI: 파라미터/중간문자 뒤에 종결 문자(0x40~0x7E)가 온다.
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)

	default:
		// ESC + 한 글자 (예: ESC b)
		return 2
	}
}

func isSingleReply(s string) bool {
	if len(s) < 2 || s[0] != 0x1b {
		return false
	}

	// 포커스 인/아웃: CSI I, CSI O
	if s == "\x1b[I" || s == "\x1b[O" {
		return true
	}

	// CSI ... 로 시작하는 응답들
	if strings.HasPrefix(s, "\x1b[") {
		body := s[2:]
		switch {
		// DA1/DA2 응답: CSI ? ... c  또는 CSI > ... c
		case (strings.HasPrefix(body, "?") || strings.HasPrefix(body, ">")) &&
			strings.HasSuffix(body, "c"):
			return true
		// 커서 위치 응답: CSI row ; col R
		case strings.HasSuffix(body, "R") && isDigitsAndSemis(body[:len(body)-1]):
			return true
		// 모드 상태 응답(DECRPM): CSI ? mode ; value $ y
		case strings.HasSuffix(body, "$y"):
			return true
		// 창 크기 응답: CSI 8 ; rows ; cols t
		case strings.HasSuffix(body, "t") && strings.HasPrefix(body, "8;"):
			return true
		}
	}

	// 색상 질의 응답: OSC 10/11/12 ... rgb:... (BEL 또는 ST로 끝)
	//
	// vi나 편집기가 뜰 때 배경/전경색을 묻고 터미널이 이걸로 답한다.
	if strings.HasPrefix(s, "\x1b]") {
		if strings.Contains(s, "rgb:") || strings.Contains(s, "rgba:") {
			return true
		}
	}

	return false
}

func isDigitsAndSemis(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != ';' {
			return false
		}
	}
	return true
}
