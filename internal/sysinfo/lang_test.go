package sysinfo

import (
	"testing"
	"time"

	"github.com/foncdev/terminal-agent/internal/lang"
)

func TestHumanDurationEnglish(t *testing.T) {
	defer lang.Set(lang.EN)()
	cases := map[time.Duration]string{
		5*24*time.Hour + 21*time.Hour: "5d 21h",
		3 * 24 * time.Hour:            "3d",
		2*time.Hour + 30*time.Minute:  "2h 30m",
		45 * time.Minute:              "45m",
	}
	for in, want := range cases {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
