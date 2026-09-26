//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// ownGroup은 명령을 새 프로세스 그룹으로 띄우고, 취소하면 그룹째 끝낸다.
//
// exec.CommandContext는 시간이 다 되면 직접 띄운 셸만 죽인다.
// `sleep 15 &`처럼 셸이 뒤에 남긴 프로세스는 살아서 출력 파이프를
// 붙잡고, 그동안 Run이 돌아오지 않는다. 2초 제한에 15초가 걸렸다.
func ownGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		// 음수 pid는 그 그룹 전체다.
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
