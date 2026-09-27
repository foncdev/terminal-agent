// Package bonjour는 같은 와이파이의 폰(Relay 앱)이 이 agent를 찾게 알린다.
//
// 맥 IP가 바뀌면 폰에 적어 둔 주소가 틀려 붙지 못했다. 이름으로 알려 두면
// 폰이 새 IP를 스스로 찾는다. 맥에 기본으로 있는 dns-sd로 알린다 — 의존성을
// 늘리지 않으려고 그렇게 한다.
package bonjour

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// ServiceType은 폰이 찾는 서비스 종류다. iOS 앱의 NSBonjourServices와 같아야 한다.
const ServiceType = "_relayterm._tcp"

// Name은 알리는 이름이다. 폰은 이 이름으로 같은 agent인지 알아본다.
func Name() string {
	host, _ := os.Hostname()
	return "terminal-agent (" + strings.TrimSuffix(host, ".local") + ")"
}

// Advertise는 알리기 시작하고, 멈추는 함수를 돌려준다.
//
// 맥이 아니거나, 맥 안(루프백)에서만 열려 있으면 알리지 않는다 — 폰이
// 찾아도 붙을 수 없다. TERMINAL_BONJOUR=false로 끌 수 있다.
func Advertise(port int, localOnly bool) func() {
	if runtime.GOOS != "darwin" || localOnly || os.Getenv("TERMINAL_BONJOUR") == "false" {
		return func() {}
	}
	cmd := exec.Command("dns-sd", "-R", Name(), ServiceType, "local", strconv.Itoa(port), "path=/")
	if err := cmd.Start(); err != nil {
		// dns-sd가 없다. 찾기만 못 할 뿐 agent는 그대로 돈다.
		return func() {}
	}
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
}
