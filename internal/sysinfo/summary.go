package sysinfo

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Get은 홈 화면에 쓸 요약을 모은다.
//
// 한 조각을 못 구해도 나머지는 준다. 안경에서 숫자 하나가 비는 것이
// 화면 전체가 오류로 바뀌는 것보다 낫다.
func Get(ctx context.Context) Summary {
	s := Summary{
		CPUPercent: -1,
		Load:       loadAvg(ctx),
		Uptime:     uptime(ctx),
		OS:         runtime.GOOS,
	}
	if h, err := os.Hostname(); err == nil {
		s.Host = h
	}
	s.MemUsedGB, s.MemTotalGB = memory(ctx)
	s.CPUPercent = cpuPercent(ctx)
	return s
}

/*
 * cpuPercent는 전체 CPU 사용률을 구한다.
 *
 * 프로세스별 %CPU를 더하지 않는다. 그 값은 코어 하나를 100으로 세기
 * 때문에 코어가 여럿이면 100을 훌쩍 넘어 사용률처럼 보이지 않는다.
 * 대신 유휴(idle) 비율을 읽어 100에서 뺀다.
 */
func cpuPercent(ctx context.Context) float64 {
	switch runtime.GOOS {
	case "darwin":
		// top -l 1은 한 번만 재고 끝난다. -l 2가 더 정확하지만
		// 첫 표본을 버리느라 1초 이상 걸려 안경이 기다린다.
		out, err := run(ctx, "top", "-l", "1", "-n", "0")
		if err != nil {
			return -1
		}
		for _, line := range strings.Split(out, "\n") {
			if !strings.HasPrefix(line, "CPU usage") {
				continue
			}
			// "CPU usage: 3.12% user, 7.81% sys, 89.06% idle"
			for _, part := range strings.Split(line, ",") {
				if !strings.Contains(part, "idle") {
					continue
				}
				f := strings.Fields(strings.TrimSpace(part))
				if len(f) == 0 {
					continue
				}
				idle, err := strconv.ParseFloat(strings.TrimSuffix(f[0], "%"), 64)
				if err != nil {
					return -1
				}
				return round1(100 - idle)
			}
		}
		return -1

	case "linux":
		// /proc/stat을 두 번 읽어 그 사이의 움직임을 본다. 한 번만
		// 읽으면 부팅 이후 누적이라 지금 상태를 알 수 없다.
		a, err := procStat()
		if err != nil {
			return -1
		}
		select {
		case <-ctx.Done():
			return -1
		case <-time.After(150 * time.Millisecond):
		}
		b, err := procStat()
		if err != nil {
			return -1
		}
		dTotal := b.total - a.total
		dIdle := b.idle - a.idle
		if dTotal <= 0 {
			return -1
		}
		return round1(100 * float64(dTotal-dIdle) / float64(dTotal))
	}
	return -1
}

type cpuTimes struct{ total, idle int64 }

func procStat() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range fields[1:] {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			t.total += n
			// 4번째(idle)와 5번째(iowait)를 쉬는 시간으로 본다.
			if i == 3 || i == 4 {
				t.idle += n
			}
		}
		return t, nil
	}
	return cpuTimes{}, sc.Err()
}

// memory는 쓰는 양과 전체를 GB로 준다. 못 구하면 둘 다 0이다.
func memory(ctx context.Context) (used, total float64) {
	switch runtime.GOOS {
	case "darwin":
		out, err := run(ctx, "sysctl", "-n", "hw.memsize")
		if err != nil {
			return 0, 0
		}
		bytes, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err != nil {
			return 0, 0
		}
		total = bytes / (1 << 30)

		/*
		 * macOS는 "쓰는 양"이 단순하지 않다. vm_stat의 페이지를 세어
		 * active + wired + compressed를 쓰는 것으로 본다. inactive는
		 * 곧 회수할 수 있어 제외한다 — 넣으면 늘 꽉 찬 것처럼 보인다.
		 */
		vm, err := run(ctx, "vm_stat")
		if err != nil {
			return 0, total
		}
		pageSize := 4096.0
		var active, wired, compressed float64
		for _, line := range strings.Split(vm, "\n") {
			if strings.Contains(line, "page size of") {
				f := strings.Fields(line)
				for i, w := range f {
					if w == "of" && i+1 < len(f) {
						if n, err := strconv.ParseFloat(f[i+1], 64); err == nil {
							pageSize = n
						}
					}
				}
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			n, err := strconv.ParseFloat(strings.Trim(strings.TrimSpace(parts[1]), "."), 64)
			if err != nil {
				continue
			}
			switch {
			case strings.Contains(parts[0], "Pages active"):
				active = n
			case strings.Contains(parts[0], "Pages wired"):
				wired = n
			case strings.Contains(parts[0], "occupied by compressor"):
				compressed = n
			}
		}
		used = (active + wired + compressed) * pageSize / (1 << 30)
		return round1(used), round1(total)

	case "linux":
		f, err := os.Open("/proc/meminfo")
		if err != nil {
			return 0, 0
		}
		defer f.Close()
		var totalKB, availKB float64
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 {
				continue
			}
			n, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				totalKB = n
			case "MemAvailable:":
				availKB = n
			}
		}
		// MemAvailable을 쓴다. MemFree만 보면 캐시가 쓰는 것으로 잡혀
		// 늘 꽉 찬 것처럼 보인다.
		return round1((totalKB - availKB) / (1 << 20)), round1(totalKB / (1 << 20))
	}
	return 0, 0
}

// loadAvg는 1·5·15분 평균이다. 윈도우에는 없는 개념이라 비워 둔다.
func loadAvg(ctx context.Context) []float64 {
	if runtime.GOOS == "windows" {
		return nil
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		out := make([]float64, 0, 3)
		for i := 0; i < 3 && i < len(f); i++ {
			if n, err := strconv.ParseFloat(f[i], 64); err == nil {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// macOS는 /proc이 없다. sysctl로 읽는다.
	out, err := run(ctx, "sysctl", "-n", "vm.loadavg")
	if err != nil {
		return nil
	}
	// "{ 1.87 2.49 2.55 }"
	fields := strings.Fields(strings.Trim(strings.TrimSpace(out), "{} "))
	res := make([]float64, 0, 3)
	for i := 0; i < 3 && i < len(fields); i++ {
		if n, err := strconv.ParseFloat(fields[i], 64); err == nil {
			res = append(res, n)
		}
	}
	return res
}

/*
 * uptime은 가동 시간을 한국어로 준다.
 *
 * uptime 명령의 글을 그대로 쓰지 않는다. 로케일과 OS마다 꼴이 달라
 * ("up 5 days, 21:55" / "up 3:20") 안경에서 읽기 어렵다. 부팅 시각을
 * 구해 직접 센다.
 */
func uptime(ctx context.Context) string {
	var boot time.Time

	switch runtime.GOOS {
	case "darwin":
		out, err := run(ctx, "sysctl", "-n", "kern.boottime")
		if err != nil {
			return ""
		}
		// "{ sec = 1757000000, usec = 0 } ..."
		i := strings.Index(out, "sec = ")
		if i < 0 {
			return ""
		}
		rest := out[i+len("sec = "):]
		end := strings.IndexAny(rest, ",} ")
		if end < 0 {
			return ""
		}
		secs, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
		if err != nil {
			return ""
		}
		boot = time.Unix(secs, 0)

	case "linux":
		b, err := os.ReadFile("/proc/uptime")
		if err != nil {
			return ""
		}
		f := strings.Fields(string(b))
		if len(f) == 0 {
			return ""
		}
		secs, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			return ""
		}
		boot = time.Now().Add(-time.Duration(secs) * time.Second)

	default:
		return ""
	}

	return humanDuration(time.Since(boot))
}

// humanDuration은 기간을 한국어 두 토막으로 줄인다.
// 안경 화면이 좁아 "3일 4시간"까지만 보여준다.
func humanDuration(d time.Duration) string {
	if d < 0 {
		return ""
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	switch {
	case days > 0:
		if hours > 0 {
			return strconv.Itoa(days) + "일 " + strconv.Itoa(hours) + "시간"
		}
		return strconv.Itoa(days) + "일"
	case hours > 0:
		if mins > 0 {
			return strconv.Itoa(hours) + "시간 " + strconv.Itoa(mins) + "분"
		}
		return strconv.Itoa(hours) + "시간"
	default:
		return strconv.Itoa(mins) + "분"
	}
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
