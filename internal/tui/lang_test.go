package tui

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/foncdev/terminal-agent/internal/lang"
	"github.com/foncdev/terminal-agent/internal/relaylink"
)

func TestStatusTextEnglish(t *testing.T) {
	defer lang.Set(lang.EN)()
	hangul := func(s string) bool {
		return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) }) >= 0
	}

	m := &Model{}
	got := []string{m.relayPart(), uptime(time.Now().Add(-90 * time.Minute))}
	m.relayOn = true
	got = append(got, m.relayPart())
	m.status = relaylink.Status{Connected: true}
	got = append(got, m.relayPart())
	m.status = relaylink.Status{LastError: "x"}
	got = append(got, m.relayPart())

	if got[0] != "Local only" || got[1] != "1h 30m" {
		t.Errorf("got %q", got)
	}
	for _, s := range got {
		if hangul(s) {
			t.Errorf("영어가 아니다: %q", s)
		}
	}
}
