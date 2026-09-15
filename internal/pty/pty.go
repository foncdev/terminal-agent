// Package pty는 플랫폼별 의사 터미널을 하나의 인터페이스로 감싼다.
//
// unix와 윈도우는 생김새가 꽤 다르다.
//   - unix    : exec.Cmd(argv 배열)를 넘기고 *os.File을 받는다.
//   - windows : ConPTY가 os/exec를 못 쓴다. 자식에게
//     PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE을 붙여야 하는데 exec.Cmd로는
//     표현이 안 돼서, 라이브러리가 CreateProcess를 직접 부른다.
//     명령도 배열이 아니라 문자열 한 줄로 받는다.
//
// 그 차이를 pty_unix.go와 pty_windows.go가 흡수하고, 바깥 코드는
// argv 배열만 다룬다. 문자열로 합치는 일은 윈도우 구현 안에서만 일어난다.
package pty

import (
	"context"
	"errors"
	"io"
)

// ErrUnsupported는 이 플랫폼에서 PTY를 쓸 수 없을 때 반환된다.
// 윈도우에서 ConPTY가 없는 경우(10 1809 미만)가 여기 해당한다.
var ErrUnsupported = errors.New("이 플랫폼에서는 터미널을 쓸 수 없습니다")

// Pty는 살아 있는 의사 터미널 하나다.
//
// Read는 화면 출력, Write는 키 입력이다. 둘 다 블로킹이므로
// 호출자가 고루틴을 나눠 쓴다.
type Pty interface {
	io.ReadWriteCloser

	// Resize는 터미널 크기를 바꾼다. 셸이 SIGWINCH를 받아 다시 그린다.
	Resize(cols, rows int) error

	// Wait는 프로세스가 끝날 때까지 기다렸다가 종료 코드를 준다.
	// ctx가 끝나면 기다리기를 그만둔다(프로세스를 죽이지는 않는다).
	Wait(ctx context.Context) (int, error)

	// Pid는 자식 프로세스 번호다. 로그와 강제 종료에 쓴다.
	Pid() int
}

// Options는 터미널을 띄울 때의 조건이다.
type Options struct {
	// Argv는 실행할 명령이다. 비어 있으면 기본 셸을 쓴다.
	// 윈도우에서는 구현 안에서 문자열로 합쳐진다.
	Argv []string

	// Dir은 시작 디렉터리다. 비어 있으면 프로세스의 현재 위치를 쓴다.
	Dir string

	// Env는 자식에게 넘길 환경변수다. 비어 있으면 부모 것을 물려준다.
	Env []string

	Cols int
	Rows int
}

// 크기를 안 주면 쓰는 값. 0을 그대로 넘기면 셸이 이상하게 그린다.
const (
	defaultCols = 80
	defaultRows = 24
)

// normalize는 빠진 값을 채운다. 각 플랫폼 구현이 시작할 때 부른다.
func (o Options) normalize() Options {
	if o.Cols <= 0 {
		o.Cols = defaultCols
	}
	if o.Rows <= 0 {
		o.Rows = defaultRows
	}
	if len(o.Argv) == 0 {
		o.Argv = []string{DefaultShell()}
	}
	return o
}

// Start는 의사 터미널을 띄운다. 구현은 플랫폼별 파일에 있다.
func Start(opts Options) (Pty, error) {
	return start(opts.normalize())
}

// Available은 이 플랫폼에서 터미널을 쓸 수 있는지 알려준다.
// 윈도우에서 ConPTY 유무를 확인하는 용도라, unix에서는 늘 true다.
func Available() bool { return available() }

// errorAs는 errors.As를 짧게 쓰려고 둔 것이다. 플랫폼 구현이 함께 쓴다.
func errorAs(err error, target any) bool { return errors.As(err, target) }
