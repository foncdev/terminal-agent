package lang

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	cases := map[string]Lang{"": KO, "ko": KO, "en": EN, "EN": EN, "en-US": EN, " en_GB ": EN, "fr": KO}
	for in, want := range cases {
		if got := Parse(in); got != want {
			t.Fatalf("Parse(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestEnvAndOverride(t *testing.T) {
	t.Setenv("RELAY_LANG", "en")
	if L("가", "a") != "a" {
		t.Fatal("RELAY_LANG=en인데 영어가 아니다")
	}
	restore := Set(KO)
	if L("가", "a") != "가" {
		t.Fatal("Set(KO)가 먹지 않았다")
	}
	restore()
	t.Setenv("RELAY_LANG", "")
	if L("가", "a") != "가" {
		t.Fatal("기본은 한국어여야 한다")
	}
}

func TestErrorFollowsLanguage(t *testing.T) {
	e := NewError("없다", "missing")
	defer Set(EN)()
	if e.Error() != "missing" {
		t.Fatalf("got %q", e.Error())
	}
	if !errors.Is(e, e) {
		t.Fatal("errors.Is가 안 된다")
	}
}
