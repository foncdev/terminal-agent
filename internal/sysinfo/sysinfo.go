// Package sysinfo는 top·ps를 안경에서 읽을 수 있는 값으로 옮긴다.
//
// top을 그대로 흘려보내지 않는다. ANSI 이스케이프가 안경 화면에서
// 깨지고, 갱신을 계속 밀어 배터리를 먹고, 한 줄이 80칸을 넘어 접힌다.
// 그래서 한 번 읽어 숫자만 뽑아 준다.
//
// PTY가 필요 없는 일이라 셸을 열지 않는다. 명령을 직접 실행하되 인자를
// 코드에 박아 둔다 — 사용자 입력이 섞이지 않으므로 주입이 성립하지 않는다.
package sysinfo

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Summary는 홈 화면 한 줄에 들어갈 값이다.
type Summary struct {
	// 0~100. 구할 수 없으면 -1.
	CPUPercent float64 `json:"cpuPercent"`
	MemUsedGB  float64 `json:"memUsedGB"`
	MemTotalGB float64 `json:"memTotalGB"`
	// 1·5·15분 평균. 윈도우는 비어 있다.
	Load []float64 `json:"load"`
	// 사람이 읽는 가동 시간. "3일 4시간" 꼴.
	Uptime string `json:"uptime"`
	Host   string `json:"host"`
	OS     string `json:"os"`
}

// Proc은 프로세스 한 줄이다.
type Proc struct {
	PID  int     `json:"pid"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Name string  `json:"name"`
}

// 명령이 매달리면 안경이 빈 화면으로 기다린다. 짧게 끊는다.
const cmdTimeout = 3 * time.Second

func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

/*
 * shortName은 실행 경로를 안경에서 읽을 이름으로 줄인다.
 *
 * ps는 전체 경로를 준다. macOS 앱은 그것이 100자를 넘어 안경 한 줄
 * (64칸)을 훨씬 넘긴다. 파일 이름만 남기고, 그래도 길면 자른다.
 *
 * 괄호 안 설명(… (Renderer) 같은 것)은 떼지 않는다. Chrome Helper가
 * 여러 개 뜰 때 그것만이 서로를 구별해 준다.
 */
func shortName(raw string, max int) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "?"
	}
	// 경로 앞부분을 버린다. 공백이 든 경로도 있어 마지막 조각만 쓴다.
	if i := strings.LastIndex(s, string(filepath.Separator)); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// Procs는 CPU를 많이 쓰는 순서로 n개를 준다.
//
// top 대신 ps를 쓴다. top은 화면을 그리려고 커서를 옮기는 문자를 섞어
// 보내서, 파싱하면 숫자에 그 찌꺼기가 붙는다.
func Procs(ctx context.Context, n int) ([]Proc, error) {
	if n <= 0 || n > 50 {
		n = 10
	}

	if runtime.GOOS == "windows" {
		return procsWindows(ctx, n)
	}

	// CPU 내림차순. 맥(BSD ps)은 -r, 리눅스(procps)는 --sort를 쓴다.
	// 리눅스 ps는 -r을 모르는 옵션으로 보고 exit 1로 끝나서, CI(ubuntu)에서
	// 프로세스 목록이 통째로 실패했다.
	args := []string{"-Ao", "pid,pcpu,pmem,comm"}
	if runtime.GOOS == "linux" {
		args = append(args, "--sort=-pcpu")
	} else {
		args = append(args, "-r")
	}
	out, err := run(ctx, "ps", args...)
	if err != nil {
		return nil, err
	}

	var procs []Proc
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// 머리글을 건너뛴다.
		if line == "" || strings.HasPrefix(line, "PID") {
			continue
		}
		// 앞 세 칸은 숫자, 나머지가 전부 이름이다. 이름에 공백이 들어
		// 있어 SplitN으로 끊어야 뒤가 잘리지 않는다.
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		cpu, err2 := strconv.ParseFloat(f[1], 64)
		mem, err3 := strconv.ParseFloat(f[2], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		// 이름은 네 번째 칸부터 줄 끝까지다.
		name := strings.Join(f[3:], " ")

		procs = append(procs, Proc{PID: pid, CPU: cpu, Mem: mem, Name: shortName(name, 40)})
		if len(procs) >= n {
			break
		}
	}
	return procs, sc.Err()
}

func procsWindows(ctx context.Context, n int) ([]Proc, error) {
	// tasklist는 CPU를 주지 않는다. 메모리 기준으로 대신한다.
	out, err := run(ctx, "tasklist", "/fo", "csv", "/nh")
	if err != nil {
		return nil, err
	}
	var procs []Proc
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\",\"")
		if len(cols) < 5 {
			continue
		}
		name := strings.Trim(cols[0], "\"")
		pid, err := strconv.Atoi(strings.Trim(cols[1], "\""))
		if err != nil {
			continue
		}
		procs = append(procs, Proc{PID: pid, CPU: -1, Name: shortName(name, 40)})
		if len(procs) >= n {
			break
		}
	}
	return procs, sc.Err()
}
