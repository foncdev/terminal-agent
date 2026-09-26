package relaylink

import "testing"

// relay가 넘긴 경로를 이 기기 주소로만 바꾸는지 본다. 예전에는 문자열을
// 이어 붙여 "@evil.example/x"가 다른 호스트로 API 키를 실어 보냈다.
func TestLocalURL(t *testing.T) {
	l := New(Options{LocalURL: "http://127.0.0.1:4200"})

	for _, p := range []string{"/terminals", "/terminals/abc/stream?token=x", "/sys/summary", "/run", "/run/inspect"} {
		got, err := l.localURL(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if got[:len("http://127.0.0.1:4200/")] != "http://127.0.0.1:4200/" {
			t.Fatalf("%s → %s", p, got)
		}
	}

	for _, p := range []string{
		"@evil.example/x",
		"//evil.example/terminals",
		"http://evil.example/terminals",
		"terminals",
		"/health",
		"/",
		"/terminals/../internal",
		"/terminalsX",
	} {
		if got, err := l.localURL(p); err == nil {
			t.Fatalf("%s: want error, got %s", p, got)
		}
	}
}
