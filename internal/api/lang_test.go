package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode"

	"github.com/foncdev/terminal-agent/internal/lang"
)

// RELAY_LANG=en이면 relay-service·폰·안경으로 가는 오류 글이 영어다.
// code 값(confirm_required 등)은 언어와 상관없이 그대로다.
func TestErrorMessagesEnglish(t *testing.T) {
	defer lang.Set(lang.EN)()
	srv, _ := newGuardServer(t, "k-123456789012345678901234")
	key := map[string]string{"x-api-key": "k-123456789012345678901234", "Content-Type": "application/json"}

	cases := []struct {
		method, path, body string
		headers            map[string]string
		code               string
	}{
		{"POST", "/run", `{"command":"rm -rf /tmp/x"}`, key, "confirm_required"},
		{"GET", "/terminals/nope", "", key, "not_found"},
		{"GET", "/terminals", "", nil, "unauthorized"},
		{"POST", "/terminals/nope/resize", `{"cols":0,"rows":0}`, key, ""},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(c.body))
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Error struct{ Code, Message string } `json:"error"`
			Risks []struct{ Reason string }      `json:"risks"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()

		if c.code != "" && body.Error.Code != c.code {
			t.Errorf("%s %s: code = %q, want %q", c.method, c.path, body.Error.Code, c.code)
		}
		texts := []string{body.Error.Message}
		for _, r := range body.Risks {
			texts = append(texts, r.Reason)
		}
		for _, s := range texts {
			if s == "" || strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) }) >= 0 {
				t.Errorf("%s %s: 영어가 아니다: %q", c.method, c.path, s)
			}
		}
	}
}
