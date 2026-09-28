package sysinfo

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/foncdev/terminal-agent/internal/lang"
)

func TestShortName(t *testing.T) {
	// ps는 전체 경로를 준다. macOS 앱은 100자를 넘어 안경 한 줄(64칸)을
	// 훌쩍 넘긴다. 파일 이름만 남겨야 읽힌다.
	long := "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/152.0/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)"
	got := shortName(long, 40)
	if strings.Contains(got, "/") {
		t.Errorf("경로가 남았다: %q", got)
	}
	if len([]rune(got)) > 40 {
		t.Errorf("상한을 넘겼다(%d): %q", len([]rune(got)), got)
	}
	// 괄호 설명은 남겨야 Chrome Helper 여러 개를 구별할 수 있다.
	if !strings.Contains(got, "Chrome Helper") {
		t.Errorf("이름이 사라졌다: %q", got)
	}

	if shortName("", 10) != "?" {
		t.Error("빈 이름을 그대로 두면 화면에 빈 줄이 생긴다")
	}
	if got := shortName("claude", 40); got != "claude" {
		t.Errorf("짧은 이름을 건드렸다: %q", got)
	}
}

func TestHumanDuration(t *testing.T) {
	defer lang.Set(lang.KO)()
	cases := []struct {
		in   time.Duration
		want string
	}{
		{5*24*time.Hour + 21*time.Hour, "5일 21시간"},
		{3 * 24 * time.Hour, "3일"},
		{2*time.Hour + 30*time.Minute, "2시간 30분"},
		{45 * time.Minute, "45분"},
		{0, "0분"},
	}
	for _, c := range cases {
		if got := humanDuration(c.in); got != c.want {
			t.Errorf("humanDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	// 음수는 시계가 틀어진 경우다. 빈 값으로 둬 화면에 안 띄운다.
	if humanDuration(-time.Hour) != "" {
		t.Error("음수 기간을 그대로 보여준다")
	}
}

func TestProcs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("이 기계에서는 ps 경로를 확인할 수 없다")
	}
	procs, err := Procs(context.Background(), 5)
	if err != nil {
		t.Fatalf("Procs: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("프로세스가 하나도 없다 — 파싱이 깨졌다")
	}
	if len(procs) > 5 {
		t.Errorf("요청보다 많이 줬다: %d", len(procs))
	}
	for _, p := range procs {
		if p.PID <= 0 {
			t.Errorf("PID가 이상하다: %+v", p)
		}
		if p.Name == "" {
			t.Errorf("이름이 비었다: %+v", p)
		}
		// 머리글(PID %CPU …)이 섞여 들어오면 이름이 숫자가 아닌 글자로 남는다.
		if strings.HasPrefix(p.Name, "%") {
			t.Errorf("머리글이 섞였다: %+v", p)
		}
	}
	// -r이 먹었는지 본다. 내림차순이 아니면 상위 프로세스라는 뜻이 없다.
	for i := 1; i < len(procs); i++ {
		if procs[i-1].CPU < procs[i].CPU {
			t.Errorf("CPU 내림차순이 아니다: %v", procs)
			break
		}
	}
}

func TestProcsClampsCount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ps 없음")
	}
	// 상한이 없으면 안경에 수백 줄을 보내려 한다.
	procs, err := Procs(context.Background(), 9999)
	if err != nil {
		t.Fatalf("Procs: %v", err)
	}
	if len(procs) > 10 {
		t.Errorf("상한을 넘겼다: %d", len(procs))
	}
}

func TestGet(t *testing.T) {
	s := Get(context.Background())

	if s.OS != runtime.GOOS {
		t.Errorf("OS = %q", s.OS)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if s.MemTotalGB <= 0 {
			t.Errorf("전체 메모리를 못 읽었다: %v", s.MemTotalGB)
		}
		if s.MemUsedGB > s.MemTotalGB {
			t.Errorf("쓰는 양이 전체보다 크다: %v / %v", s.MemUsedGB, s.MemTotalGB)
		}
		if s.Uptime == "" {
			t.Error("가동 시간을 못 읽었다")
		}
		if len(s.Load) == 0 {
			t.Error("로드를 못 읽었다")
		}
		// -1은 '못 구했다'는 뜻. 그 외에는 0~100이어야 한다.
		if s.CPUPercent != -1 && (s.CPUPercent < 0 || s.CPUPercent > 100) {
			t.Errorf("CPU 사용률이 범위를 벗어났다: %v", s.CPUPercent)
		}
	}
}

func TestGetRespectsTimeout(t *testing.T) {
	// 명령이 매달리면 안경이 빈 화면으로 기다린다. 끊긴 맥락에서도
	// 화면을 그릴 값은 돌려줘야 한다.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Get(ctx)
	if s.OS == "" {
		t.Error("맥락이 끊겼다고 빈 값을 줬다 — OS는 명령 없이 알 수 있다")
	}
}
