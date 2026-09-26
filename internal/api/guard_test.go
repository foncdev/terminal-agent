package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foncdev/terminal-agent/internal/api"
	"github.com/foncdev/terminal-agent/internal/config"
	"github.com/foncdev/terminal-agent/internal/terminal"
)

// 브라우저와 DNS 리바인딩으로 셸을 부르지 못하는지 본다.
//
// 예전에는 Origin을 되비추고 본문을 Content-Type과 상관없이 JSON으로
// 읽었다. 키가 없으면 아무 웹페이지가 text/plain POST 한 번으로 /run을
// 돌려 파일을 만들 수 있었다(실제로 재현했다).

func newGuardServer(t *testing.T, key string, origins ...string) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	reg := terminal.NewRegistry(terminal.RegistryOptions{Max: 2, Scrollback: 4096})
	t.Cleanup(reg.CloseAll)
	srv := httptest.NewServer(api.NewServer(config.Config{
		AllowedRoots: []string{dir},
		APIKey:       key,
		CORSOrigins:  origins,
		MaxTerminals: 2,
	}, reg))
	t.Cleanup(srv.Close)
	return srv, dir
}

func do(t *testing.T, method, url string, headers map[string]string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestOtherSiteCannotRunCommands(t *testing.T) {
	srv, dir := newGuardServer(t, "")
	marker := filepath.Join(dir, "pwned")
	body := `{"command":"touch ` + marker + `"}`

	res := do(t, "POST", srv.URL+"/run", map[string]string{
		"Origin":       "https://evil.example",
		"Content-Type": "text/plain",
	}, body)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS 헤더를 주면 안 된다")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("다른 사이트의 요청으로 명령이 돌았다")
	}

	pre := do(t, "OPTIONS", srv.URL+"/run", map[string]string{"Origin": "https://evil.example"}, "")
	if pre.StatusCode != http.StatusForbidden {
		t.Fatalf("preflight status = %d, want 403", pre.StatusCode)
	}
}

func TestJSONBodyOnly(t *testing.T) {
	srv, dir := newGuardServer(t, "")
	marker := filepath.Join(dir, "plain")
	res := do(t, "POST", srv.URL+"/run", map[string]string{"Content-Type": "text/plain"},
		`{"command":"touch `+marker+`"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("text/plain 본문으로 명령이 돌았다")
	}

	ok := do(t, "POST", srv.URL+"/run/inspect", map[string]string{"Content-Type": "application/json; charset=utf-8"},
		`{"command":"ls"}`)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("JSON 요청이 막혔다: %d", ok.StatusCode)
	}
}

func TestAllowedOriginPasses(t *testing.T) {
	srv, _ := newGuardServer(t, "", "https://ok.example")
	res := do(t, "GET", srv.URL+"/terminals", map[string]string{"Origin": "https://ok.example"}, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://ok.example" {
		t.Fatalf("ACAO = %q", got)
	}
}

func TestNoKeyRejectsForeignHost(t *testing.T) {
	srv, _ := newGuardServer(t, "")
	for _, host := range []string{"attacker.example", "127.evil.example", "127.0.0.1.nip.io", "192.168.0.10"} {
		res := do(t, "GET", srv.URL+"/terminals", map[string]string{"Host": host}, "")
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("Host %s: status = %d, want 403", host, res.StatusCode)
		}
	}
	for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
		res := do(t, "GET", srv.URL+"/terminals", map[string]string{"Host": host}, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("Host %s: status = %d, want 200", host, res.StatusCode)
		}
	}
}

func TestKeyedServerAcceptsAnyHost(t *testing.T) {
	// 키가 있으면 LAN 주소로도 쓴다. 인증이 막는다.
	srv, _ := newGuardServer(t, "k")
	res := do(t, "GET", srv.URL+"/terminals", map[string]string{"Host": "192.168.0.10", "x-api-key": "k"}, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
}
