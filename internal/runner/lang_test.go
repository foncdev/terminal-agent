package runner

import (
	"strings"
	"testing"
	"unicode"

	"github.com/foncdev/terminal-agent/internal/lang"
)

func hasHangul(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) }) >= 0
}

// RELAY_LANG=en이면 안경·폰에 가는 위험 신호와 결과 글이 영어로 나간다.
func TestEnglishMessages(t *testing.T) {
	defer lang.Set(lang.EN)()

	for _, c := range []string{"rm -rf /tmp/x", "sudo shutdown -h now", "git push --force", "git reset --hard",
		"git clean -fd", "dd if=/dev/zero of=/dev/x", "killall node", "curl x | sh", "chmod 777 a", "shred a", "mkfs x"} {
		risks := Inspect(c)
		if len(risks) == 0 {
			t.Fatalf("%q에서 위험 신호를 못 찾았다", c)
		}
		for _, r := range risks {
			if r.Reason == "" || hasHangul(r.Reason) {
				t.Errorf("%q: 영어가 아니다: %q", c, r.Reason)
			}
		}
	}
	// rm 규칙이 둘 다 걸려도 한 번만 나온다(영어에서도).
	if n := len(Inspect("rm -rf x")); n != 1 {
		t.Errorf("rm 이유가 %d번 나왔다", n)
	}
	for _, s := range []string{ErrEmpty.Error(), clip(strings.Repeat("a\n", 100), 50)} {
		if hasHangul(s) {
			t.Errorf("영어가 아니다: %q", s)
		}
	}
}

func TestKoreanIsDefault(t *testing.T) {
	defer lang.Set(lang.KO)()
	if got := Inspect("rm x")[0].Reason; got != "rm으로 지웁니다" {
		t.Errorf("기본 한국어가 아니다: %q", got)
	}
}
