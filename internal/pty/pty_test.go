package pty

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TERM이 없으면 붙는 프로그램이 화면을 못 그린다. tput은 "No value for
// $TERM"으로 죽고 vi는 뜨지 않는다. 서비스나 CI처럼 터미널 없이 뜬
// 부모에서는 이 값이 비어 있어서, PTY를 만들 때 채워 줘야 한다.
func TestWithTermFillsMissing(t *testing.T) {
	got := withTerm([]string{"PATH=/usr/bin"})

	if !slices.Contains(got, "TERM="+defaultTerm) {
		t.Fatalf("TERM이 채워지지 않았다: %v", got)
	}
	// 원래 있던 것을 잃으면 안 된다.
	if !slices.Contains(got, "PATH=/usr/bin") {
		t.Fatalf("기존 환경변수가 사라졌다: %v", got)
	}
}

// 부르는 쪽이 정한 값이 우선이다.
func TestWithTermKeepsExisting(t *testing.T) {
	got := withTerm([]string{"TERM=dumb", "PATH=/usr/bin"})

	if !slices.Contains(got, "TERM=dumb") {
		t.Fatalf("지정한 TERM이 바뀌었다: %v", got)
	}
	for _, kv := range got {
		if kv == "TERM="+defaultTerm {
			t.Fatalf("기본값이 덧붙었다: %v", got)
		}
	}
}

// TERM으로 끝나는 다른 이름(COLORTERM 등)을 TERM으로 착각하면 안 된다.
func TestWithTermIgnoresSimilarNames(t *testing.T) {
	got := withTerm([]string{"COLORTERM=truecolor"})

	if !slices.Contains(got, "TERM="+defaultTerm) {
		t.Fatalf("COLORTERM을 TERM으로 봤다: %v", got)
	}
}

// Env가 비면 부모 환경을 물려받는다는 뜻이다. 부모에 TERM이 있으면
// 그대로 두고(빈 채로 넘겨 상속시키고), 없을 때만 채운다.
func TestWithTermInheritsFromParent(t *testing.T) {
	t.Setenv("TERM", "screen")
	if got := withTerm(nil); len(got) != 0 {
		t.Fatalf("부모에 TERM이 있으면 상속에 맡겨야 한다: %v", got)
	}

	t.Setenv("TERM", "")
	got := withTerm(nil)
	if !slices.Contains(got, "TERM="+defaultTerm) {
		t.Fatalf("부모에 TERM이 없으면 채워야 한다")
	}
	// 부모 환경도 함께 넘어가야 한다. PATH가 없으면 셸이 명령을 못 찾는다.
	if os.Getenv("PATH") != "" {
		var hasPath bool
		for _, kv := range got {
			if strings.HasPrefix(kv, "PATH=") {
				hasPath = true
				break
			}
		}
		if !hasPath {
			t.Fatal("부모 환경이 함께 넘어가지 않았다")
		}
	}
}

// normalize가 실제로 이 보정을 거치는지 본다. 여기를 빠뜨리면
// withTerm이 있어도 아무 일도 하지 않는다.
func TestNormalizeAppliesTerm(t *testing.T) {
	t.Setenv("TERM", "")

	got := Options{Env: []string{"PATH=/usr/bin"}}.normalize()
	if !slices.Contains(got.Env, "TERM="+defaultTerm) {
		t.Fatalf("normalize가 TERM을 채우지 않았다: %v", got.Env)
	}
}
