package bonjour

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalOnlyIsNotAdvertised(t *testing.T) {
	// 맥 안에서만 열려 있으면 폰이 찾아도 붙을 수 없다. 알리지 않는다.
	stop := Advertise(45200, true)
	stop()
}

func TestNameHasHost(t *testing.T) {
	if !strings.HasPrefix(Name(), "terminal-agent (") {
		t.Fatalf("이름이 이상하다: %s", Name())
	}
}

func TestAdvertiseOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("맥에서만 알린다")
	}
	stop := Advertise(45201, false)
	defer stop()
	// dns-sd -B로 찾아본다. 등록까지 잠깐 걸린다.
	browse := exec.Command("dns-sd", "-B", ServiceType, "local")
	var out strings.Builder
	browse.Stdout = &out
	if err := browse.Start(); err != nil {
		t.Skip("dns-sd가 없다")
	}
	time.Sleep(2500 * time.Millisecond)
	_ = browse.Process.Kill()
	_ = browse.Wait()
	if !strings.Contains(out.String(), "terminal-agent (") {
		t.Fatalf("찾지 못했다:\n%s", out.String())
	}
}
