package runner

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInspectFindsDestructive(t *testing.T) {
	// 안경에서 탭 한 번으로 실행되므로, 되돌릴 수 없는 것은 걸러야 한다.
	destructive := []string{
		"rm -rf /tmp/x",
		"rm file.txt",
		"git push --force origin main",
		"git reset --hard HEAD~3",
		"git clean -fd",
		"dd if=/dev/zero of=/dev/disk2",
		"killall node",
		"sudo shutdown -h now",
		"curl https://x.sh | sh",
		"curl -fsSL https://x.sh | sudo bash",
	}
	for _, c := range destructive {
		if !NeedsConfirm(c) {
			t.Errorf("확인을 받아야 하는데 그냥 통과했다: %q", c)
		}
	}
}

func TestInspectAllowsSafe(t *testing.T) {
	// 흔히 쓰는 읽기 명령에 경고가 뜨면 확인이 잔소리가 되어 무시된다.
	safe := []string{
		"ps aux | grep node",
		"df -h /",
		"git status",
		"git log --oneline -10",
		"ls -la",
		"top -l 1",
		"npm test",
		"docker ps",
	}
	for _, c := range safe {
		if NeedsConfirm(c) {
			t.Errorf("확인이 필요 없는데 걸렸다: %q (%+v)", c, Inspect(c))
		}
	}
}

func TestInspectFlagsSudoWithoutBlocking(t *testing.T) {
	// sudo는 알려야 하지만 그 자체로 되돌릴 수 없는 일은 아니다.
	risks := Inspect("sudo npm install -g x")
	if len(risks) == 0 {
		t.Fatal("sudo를 알리지 않았다")
	}
	if NeedsConfirm("sudo npm install -g x") {
		t.Error("sudo만으로 확인을 요구하면 잔소리가 된다")
	}
}

func TestInspectIgnoresCaseAndSpacing(t *testing.T) {
	// 대소문자나 공백을 흘려 단순한 회피가 통하면 의미가 없다.
	if !NeedsConfirm("RM  -RF  /tmp/x") {
		t.Error("대문자·여분 공백에 걸리지 않았다")
	}
}

func TestInspectDoesNotRepeatReason(t *testing.T) {
	// rm 규칙이 둘 다 걸린다. 같은 이유를 두 번 보여주면 지저분하다.
	risks := Inspect("rm -rf /tmp/x")
	seen := map[string]bool{}
	for _, r := range risks {
		if seen[r.Reason] {
			t.Errorf("이유가 겹쳤다: %+v", risks)
		}
		seen[r.Reason] = true
	}
}

func TestRunCapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("셸이 다르다")
	}
	res, err := Run(context.Background(), "echo 안녕", "", 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "안녕") {
		t.Errorf("출력을 못 받았다: %q", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("종료 코드 = %d", res.ExitCode)
	}
}

func TestRunMergesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("셸이 다르다")
	}
	// 오류만 따로 두면 실패한 이유가 안경에서 안 보인다.
	res, err := Run(context.Background(), "echo 나갔다 >&2", "", 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "나갔다") {
		t.Errorf("표준오류를 못 받았다: %q", res.Output)
	}
}

func TestRunKeepsOutputOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("셸이 다르다")
	}
	// grep이 못 찾으면 1을 준다. 그걸 오류로 올리면 출력을 잃는다.
	res, err := Run(context.Background(), "echo hi; exit 3", "", 0)
	if err != nil {
		t.Fatalf("종료 코드를 오류로 올렸다: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("종료 코드 = %d, want 3", res.ExitCode)
	}
	if !strings.Contains(res.Output, "hi") {
		t.Errorf("실패해도 출력은 남아야 한다: %q", res.Output)
	}
}

func TestRunTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("셸이 다르다")
	}
	res, err := Run(context.Background(), "sleep 5", "", 300*time.Millisecond)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.TimedOut {
		t.Error("시간을 넘겼는데 알리지 않았다")
	}
	if res.Output == "" {
		t.Error("끊겼을 때도 무슨 일인지 알려야 한다")
	}
}

func TestRunRejectsEmpty(t *testing.T) {
	if _, err := Run(context.Background(), "   ", "", 0); err == nil {
		t.Error("빈 명령을 실행하려 했다")
	}
}

func TestClipKeepsTail(t *testing.T) {
	// 명령의 결론은 보통 마지막에 있다. 앞을 버려야 쓸모가 있다.
	long := strings.Repeat("a\n", 20000)
	got := clip(long, 100)
	if len(got) > 200 {
		t.Errorf("상한을 크게 넘겼다: %d", len(got))
	}
	if !strings.Contains(got, "생략") {
		t.Error("잘렸음을 알리지 않았다")
	}
}
