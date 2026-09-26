package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Result는 한 번 실행한 결과다.
type Result struct {
	// 표준출력과 표준오류를 합친 글. 안경에서 읽을 만큼 잘려 온다.
	Output string `json:"output"`
	// 프로세스 종료 코드. 죽이지 못했으면 -1.
	ExitCode int `json:"exitCode"`
	// 시간을 넘겨 끊었는지.
	TimedOut bool `json:"timedOut"`
	// 걸린 시간(밀리초).
	TookMs int64 `json:"tookMs"`
}

// 출력 상한. 안경도 폰도 통째로는 못 읽고, 무한정 받으면 메모리를 먹는다.
const maxOutput = 16 * 1024

// 기본 제한 시간. 오래 걸리는 것은 스니펫으로 맞지 않다.
const defaultTimeout = 20 * time.Second

var ErrEmpty = errors.New("실행할 명령이 없습니다")

/*
 * Run은 명령을 셸에 넘겨 한 번 실행한다.
 *
 * 셸을 거치는 이유는 파이프와 리다이렉트를 쓰고 싶기 때문이다
 * ("ps aux | grep node" 같은 것). 대신 명령은 사용자가 직접 등록한
 * 것만 들어온다 — 바깥에서 온 글을 여기 넣으면 그대로 실행된다.
 *
 * dir이 비면 홈에서 돈다. 호출하는 쪽이 허용된 경로인지 먼저 본다.
 */
func Run(ctx context.Context, command, dir string, timeout time.Duration) (Result, error) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return Result{}, ErrEmpty
	}
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/c", cmd)
	} else {
		// -l을 주지 않는다. 로그인 셸은 프로필을 읽어 느리고, 사람마다
		// 다른 설정이 섞여 결과가 기계마다 달라진다.
		c = exec.CommandContext(ctx, shellPath(), "-c", cmd)
	}

	if dir != "" {
		c.Dir = dir
	} else if home, err := os.UserHomeDir(); err == nil {
		c.Dir = home
	}

	/*
	 * 출력을 합쳐 받는다.
	 *
	 * 안경 화면에서는 어느 쪽으로 나왔는지 구분할 방법이 없고, 오류만
	 * 따로 두면 실패한 이유가 안 보인다. 실제로 필요한 것은 "무슨 일이
	 * 있었나"라서 순서대로 섞는 편이 읽기 쉽다.
	 */
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	// 입력을 막는다. 열어두면 무언가를 묻는 명령이 영원히 기다린다.
	c.Stdin = nil

	started := time.Now()
	err := c.Run()
	took := time.Since(started)

	res := Result{
		Output:   clip(buf.String(), maxOutput),
		ExitCode: c.ProcessState.ExitCode(),
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		TookMs:   took.Milliseconds(),
	}

	if res.TimedOut {
		// 끊긴 것도 결과다. 오류로 올리면 그때까지 나온 글을 잃는다.
		if res.Output == "" {
			res.Output = "(시간을 넘겨 중단했습니다)"
		}
		return res, nil
	}

	// 종료 코드가 0이 아닌 것은 실패가 아니라 결과다. grep이 못 찾으면
	// 1을 주는데, 그걸 오류로 올리면 출력을 보여줄 수 없다.
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		return res, err
	}
	return res, nil
}

func shellPath() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// clip은 글을 상한에 맞춰 자른다. 뒤쪽을 남긴다 — 명령의 결론이 보통
// 마지막에 있다.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[len(s)-max:]
	// 여러 바이트 글자가 반으로 잘렸을 수 있다. 첫 줄바꿈까지 버린다.
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < 200 {
		cut = cut[i+1:]
	}
	return "…(앞부분 생략)\n" + cut
}
