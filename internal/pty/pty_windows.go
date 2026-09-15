//go:build windows

package pty

import (
	"context"
	"os"
	"strings"
	"syscall"

	"github.com/UserExistsError/conpty"
)

// DefaultShell은 PowerShell을 우선한다. 없으면 cmd로 떨어진다.
func DefaultShell() string {
	if s := os.Getenv("COMSPEC"); s != "" && strings.Contains(strings.ToLower(s), "powershell") {
		return s
	}
	if _, err := os.Stat(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`); err == nil {
		return `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	}
	if s := os.Getenv("COMSPEC"); s != "" {
		return s
	}
	return `C:\Windows\System32\cmd.exe`
}

// available은 ConPTY가 있는지 본다. Windows 10 1809 미만이면 없다.
func available() bool { return conpty.IsConPtyAvailable() }

// buildCommandLine은 argv를 윈도우 명령줄 문자열로 합친다.
//
// 윈도우 API는 명령을 문자열로만 받는다. 여기서만 합치고, 바깥 코드는
// 계속 배열로 다룬다. 인용은 syscall.EscapeArg에 맡긴다 — 규칙이
// 까다로워서(역슬래시와 따옴표가 겹칠 때) 직접 짜면 틀리기 쉽다.
func buildCommandLine(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = syscall.EscapeArg(a)
	}
	return strings.Join(parts, " ")
}

type winPty struct {
	c *conpty.ConPty
}

func start(o Options) (Pty, error) {
	if !available() {
		return nil, ErrUnsupported
	}

	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(o.Cols, o.Rows),
	}
	if o.Dir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(o.Dir))
	}
	if len(o.Env) > 0 {
		opts = append(opts, conpty.ConPtyEnv(o.Env))
	}

	c, err := conpty.Start(buildCommandLine(o.Argv), opts...)
	if err != nil {
		return nil, err
	}
	return &winPty{c: c}, nil
}

func (p *winPty) Read(b []byte) (int, error)  { return p.c.Read(b) }
func (p *winPty) Write(b []byte) (int, error) { return p.c.Write(b) }
func (p *winPty) Close() error                { return p.c.Close() }
func (p *winPty) Pid() int                    { return p.c.Pid() }

func (p *winPty) Resize(cols, rows int) error { return p.c.Resize(cols, rows) }

func (p *winPty) Wait(ctx context.Context) (int, error) {
	code, err := p.c.Wait(ctx)
	if err != nil {
		return -1, err
	}
	return int(code), nil
}
