//go:build windows

package runner

import "os/exec"

// ownGroup은 윈도우에서는 따로 하는 일이 없다. 남은 자식이 파이프를
// 붙잡는 경우는 Run의 WaitDelay가 끊는다.
func ownGroup(c *exec.Cmd) {}
