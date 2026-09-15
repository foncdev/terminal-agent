//go:build !windows

package pty

import (
	"context"
	"os"
	"os/exec"

	creack "github.com/creack/pty"
)

// DefaultShell은 로그인 셸을 고른다. 없으면 sh로 떨어진다.
func DefaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

func available() bool { return true }

// unixPty는 creack/pty가 준 파일 하나로 읽기·쓰기를 모두 한다.
type unixPty struct {
	f   *os.File
	cmd *exec.Cmd
}

func start(o Options) (Pty, error) {
	cmd := exec.Command(o.Argv[0], o.Argv[1:]...)
	cmd.Dir = o.Dir
	if len(o.Env) > 0 {
		cmd.Env = o.Env
	}

	f, err := creack.StartWithSize(cmd, &creack.Winsize{
		Cols: uint16(o.Cols),
		Rows: uint16(o.Rows),
	})
	if err != nil {
		return nil, err
	}
	return &unixPty{f: f, cmd: cmd}, nil
}

func (p *unixPty) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPty) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *unixPty) Resize(cols, rows int) error {
	return creack.Setsize(p.f, &creack.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *unixPty) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Close는 마스터를 닫고 자식을 죽인다.
//
// 마스터만 닫으면 셸이 EOF를 보고 알아서 끝나는 게 보통이지만,
// 입력을 안 읽는 프로그램(vim 등)이 떠 있으면 남는다. 확실히 정리한다.
func (p *unixPty) Close() error {
	err := p.f.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return err
}

func (p *unixPty) Wait(ctx context.Context) (int, error) {
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case err := <-done:
		var ee *exec.ExitError
		if errorAs(err, &ee) {
			return ee.ExitCode(), nil
		}
		if err != nil {
			return -1, err
		}
		return 0, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}
