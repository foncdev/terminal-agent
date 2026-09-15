package terminal

// ringBuffer는 최근 출력 일정량만 들고 있는다.
//
// 터미널 출력은 끝이 없으므로 전부 쌓으면 메모리가 샌다. 오래된 것부터
// 버리고 최근 것만 남긴다. 나중에 붙은 클라이언트에게 화면을 되살리는 데 쓴다.
//
// 바이트 단위로 자르므로 UTF-8 문자 중간이나 ANSI 시퀀스 중간에서
// 끊길 수 있다. 잘린 앞부분은 터미널 에뮬레이터가 알아서 무시하므로
// 실제로는 문제가 되지 않는다.
type ringBuffer struct {
	buf []byte
	// full은 한 바퀴 돌아 앞부분을 덮어쓰기 시작했는지다.
	full bool
	pos  int
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		size = 1
	}
	return &ringBuffer{buf: make([]byte, size)}
}

func (r *ringBuffer) write(p []byte) {
	if len(p) == 0 {
		return
	}

	// 한 번에 버퍼보다 큰 게 오면 뒷부분만 남긴다.
	if len(p) >= len(r.buf) {
		copy(r.buf, p[len(p)-len(r.buf):])
		r.pos = 0
		r.full = true
		return
	}

	n := copy(r.buf[r.pos:], p)
	if n < len(p) {
		// 끝에 닿았으니 앞으로 돌아가 나머지를 쓴다.
		copy(r.buf, p[n:])
		r.full = true
		r.pos = len(p) - n
		return
	}

	r.pos += n
	if r.pos == len(r.buf) {
		r.pos = 0
		r.full = true
	}
}

// bytes는 오래된 것부터 순서대로 이어 붙여 준다.
func (r *ringBuffer) bytes() []byte {
	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}

	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return out
}
