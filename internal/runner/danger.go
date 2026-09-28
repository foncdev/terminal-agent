// Package runner는 미리 등록한 명령을 한 번 실행하고 글을 돌려준다.
//
// 셸(PTY)과 다르다. 대화하지 않고, 끝나면 출력만 준다. 안경은 입력이
// 탭·스크롤 네 가지뿐이라 실셸을 쓸 수 없어서, 미리 등록해 둔 것을
// 고르는 형태로 대신한다.
package runner

import (
	"regexp"
	"strings"

	"github.com/foncdev/terminal-agent/internal/lang"
)

/*
 * 위험해 보이는 명령을 찾아 알려준다.
 *
 * 막지는 않는다. 자기 기계에서 자기 명령을 돌리는 것이라 무엇이
 * 필요한지는 사용자가 안다. 다만 안경에서 탭 한 번으로 실행되므로,
 * 되돌릴 수 없는 것은 실행 전에 한 번 더 묻는 편이 맞다.
 *
 * 여기서 걸러낸다고 안전해지는 것은 아니다. 셸은 표현이 무한해서
 * 우회하는 길이 늘 있다. 이건 실수를 줄이는 장치이지 보안 경계가 아니다.
 */

// Risk는 명령에서 찾은 위험 신호다.
type Risk struct {
	// 무엇이 걸렸는지. 사용자에게 보여준다.
	Reason string `json:"reason"`
	// 되돌릴 수 없는 일인지. 참이면 확인을 받는다.
	Destructive bool `json:"destructive"`
}

var patterns = []struct {
	re       *regexp.Regexp
	reasonKo string
	reasonEn string
	destr    bool
}{
	// 지우기. -r/-f가 붙으면 되돌릴 수 없다.
	{regexp.MustCompile(`\brm\s+(-\w*[rf]\w*\s+)+`), "rm으로 지웁니다", "Deletes with rm", true},
	{regexp.MustCompile(`\brm\s+`), "rm으로 지웁니다", "Deletes with rm", true},
	{regexp.MustCompile(`\b(shred|srm)\b`), "파일을 덮어써 지웁니다", "Overwrites files to erase them", true},

	// 권한 상승. 무엇이든 할 수 있게 된다.
	{regexp.MustCompile(`\b(sudo|doas|su)\b`), "관리자 권한으로 실행합니다", "Runs with admin rights", false},

	// 디스크·파일시스템. 실수하면 복구가 어렵다.
	{regexp.MustCompile(`\b(mkfs|fdisk|diskutil|parted)\b`), "디스크를 건드립니다", "Touches disks", true},
	{regexp.MustCompile(`\bdd\b.*\bof=`), "dd로 덮어씁니다", "Overwrites with dd", true},

	// 프로세스를 죽인다.
	{regexp.MustCompile(`\b(kill|killall|pkill)\b`), "프로세스를 종료합니다", "Kills processes", true},
	{regexp.MustCompile(`\b(shutdown|reboot|halt)\b`), "시스템을 끕니다", "Shuts down the system", true},

	// 되돌릴 수 없는 git 조작.
	{regexp.MustCompile(`\bgit\s+(push\s+.*--force|push\s+-f\b)`), "강제 푸시입니다", "Force push", true},
	{regexp.MustCompile(`\bgit\s+reset\s+--hard\b`), "작업 내용을 버립니다", "Discards your changes", true},
	{regexp.MustCompile(`\bgit\s+clean\s+-\w*f`), "추적하지 않는 파일을 지웁니다", "Deletes untracked files", true},

	// 받아서 바로 실행. 무엇이 올지 모른다.
	{regexp.MustCompile(`\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(sh|bash|zsh)\b`), "받아서 바로 실행합니다", "Downloads and runs right away", true},

	// 권한을 통째로 바꾼다.
	{regexp.MustCompile(`\bchmod\s+(-R\s+)?777\b`), "누구나 쓸 수 있게 바꿉니다", "Makes it writable by anyone", false},
}

// Inspect는 명령에서 위험 신호를 모은다. 없으면 빈 조각이다.
func Inspect(command string) []Risk {
	// 대소문자와 여분 공백을 지워 단순한 회피를 막는다. 완전하지는 않다.
	c := strings.ToLower(strings.Join(strings.Fields(command), " "))

	var out []Risk
	seen := map[string]bool{}
	for _, p := range patterns {
		if !p.re.MatchString(c) {
			continue
		}
		// 같은 이유를 두 번 보여주지 않는다. rm 규칙이 둘 다 걸린다.
		reason := lang.L(p.reasonKo, p.reasonEn)
		if seen[reason] {
			continue
		}
		seen[reason] = true
		out = append(out, Risk{Reason: reason, Destructive: p.destr})
	}
	return out
}

// NeedsConfirm은 실행 전에 확인을 받아야 하는지 알려준다.
func NeedsConfirm(command string) bool {
	for _, r := range Inspect(command) {
		if r.Destructive {
			return true
		}
	}
	return false
}
