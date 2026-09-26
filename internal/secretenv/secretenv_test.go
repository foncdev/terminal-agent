package secretenv

import (
	"slices"
	"testing"
)

func TestFilterDropsServiceSecrets(t *testing.T) {
	got := Filter([]string{
		"PATH=/usr/bin",
		"TERMINAL_API_KEY=secret",
		"RELAY_TERMINAL_TOKEN=secret",
		"RELAY_URL=ws://x",
		"HOME=/Users/me",
		"TERM=xterm-256color",
	})
	want := []string{"PATH=/usr/bin", "HOME=/Users/me", "TERM=xterm-256color"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
