package api_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/foncdev/terminal-agent/internal/api"
	"github.com/foncdev/terminal-agent/internal/config"
	"github.com/foncdev/terminal-agent/internal/terminal"
)

// 브라우저 클라이언트가 쓰는 경로를 그대로 따라가 본다.
//
// 터미널 생성 → SSE 구독 → 입력 → 리사이즈 → 닫기까지 한 번에 확인한다.
// 서버 쪽에서 이 흐름이 깨지면 브라우저를 열기 전에 여기서 잡힌다.
func TestWebClientFlow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	dir := t.TempDir()
	cfg := config.Config{
		AllowedRoots: []string{dir},
		APIKey:       "webkey",
		MaxTerminals: 3,
		Scrollback:   64 * 1024,
	}

	reg := terminal.NewRegistry(terminal.RegistryOptions{Max: 3, Scrollback: 64 * 1024})
	defer reg.CloseAll()

	srv := httptest.NewServer(api.NewServer(cfg, reg))
	defer srv.Close()

	c := &client{t: t, base: srv.URL, key: "webkey"}

	// 1. 터미널 생성 — api.createTerminal
	id := c.create(t, dir)

	// 2. 출력 구독 — streamTerminal. 인증은 ?token= 으로 간다.
	//    EventSource가 헤더를 못 붙이기 때문이다.
	events := make(chan string, 256)
	stop := c.stream(t, id, events)
	defer stop()

	// 3. 입력 — api.sendTerminalInput
	c.input(t, id, "echo W\"EB\"-OK; echo 웹한글\n")

	got := collectSSE(t, events, "WEB-OK", 15*time.Second)
	if !strings.Contains(got, "WEB-OK") {
		t.Fatalf("명령 결과가 SSE로 오지 않았다:\n%s", got)
	}
	if !strings.Contains(got, "웹한글") {
		t.Fatalf("한글이 깨졌다:\n%s", got)
	}

	// 4. 리사이즈 — xterm이 fit한 뒤 부른다
	c.resize(t, id, 132, 42)

	info := c.get(t, id)
	if info.Cols != 132 || info.Rows != 42 {
		t.Fatalf("리사이즈가 반영되지 않았다: %dx%d", info.Cols, info.Rows)
	}

	// 셸도 새 크기를 봐야 한다.
	c.input(t, id, "echo S\"Z\"=$(tput cols)x$(tput lines)\n")
	got = collectSSE(t, events, "SZ=132x42", 15*time.Second)
	if !strings.Contains(got, "SZ=132x42") {
		t.Fatalf("셸이 새 크기를 못 봤다:\n%s", got)
	}

	// 5. 닫기 — api.closeTerminal
	c.close(t, id)

	if len(c.list(t)) != 0 {
		t.Fatal("닫았는데 목록에 남아 있다")
	}
}

// 인증이 없으면 터미널 경로가 열리면 안 된다.
func TestWebClientNeedsAuth(t *testing.T) {
	reg := terminal.NewRegistry(terminal.RegistryOptions{Max: 1})
	defer reg.CloseAll()

	srv := httptest.NewServer(api.NewServer(
		config.Config{APIKey: "webkey", AllowedRoots: []string{t.TempDir()}}, reg))
	defer srv.Close()

	for _, path := range []string{"/terminals", "/terminals/x/stream"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: 인증 없이 통과했다 (%d)", path, res.StatusCode)
		}
	}
}

// --- 웹 클라이언트 흉내 ---

type client struct {
	t    *testing.T
	base string
	key  string
}

func (c *client) do(t *testing.T, method, path, body string) *http.Response {
	t.Helper()

	var r *http.Request
	var err error
	if body == "" {
		r, err = http.NewRequest(method, c.base+path, nil)
	} else {
		r, err = http.NewRequest(method, c.base+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("x-api-key", c.key)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (c *client) create(t *testing.T, dir string) string {
	t.Helper()

	res := c.do(t, "POST", "/terminals",
		`{"dir":"`+dir+`","cols":100,"rows":30}`)
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		// 컨테이너에는 /dev/ptmx가 없는 경우가 있다. PTY를 못 여는 곳에서는
		// 이 테스트가 성립하지 않으므로 건너뛴다.
		if strings.Contains(string(body), "ptmx") {
			t.Skipf("PTY를 열 수 없는 환경이다: %s", body)
		}
		t.Fatalf("생성 실패: %d %s", res.StatusCode, body)
	}

	var out struct {
		Terminal terminal.Info `json:"terminal"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Terminal.ID
}

func (c *client) get(t *testing.T, id string) terminal.Info {
	t.Helper()

	res := c.do(t, "GET", "/terminals/"+id, "")
	defer res.Body.Close()

	var out struct {
		Terminal terminal.Info `json:"terminal"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Terminal
}

func (c *client) list(t *testing.T) []terminal.Info {
	t.Helper()

	res := c.do(t, "GET", "/terminals", "")
	defer res.Body.Close()

	var out struct {
		Terminals []terminal.Info `json:"terminals"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Terminals
}

func (c *client) input(t *testing.T, id, data string) {
	t.Helper()

	b, _ := json.Marshal(map[string]string{"data": data})
	res := c.do(t, "POST", "/terminals/"+id+"/input", string(b))
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("입력 실패: %d", res.StatusCode)
	}
}

func (c *client) resize(t *testing.T, id string, cols, rows int) {
	t.Helper()

	b, _ := json.Marshal(map[string]int{"cols": cols, "rows": rows})
	res := c.do(t, "POST", "/terminals/"+id+"/resize", string(b))
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("리사이즈 실패: %d", res.StatusCode)
	}
}

func (c *client) close(t *testing.T, id string) {
	t.Helper()

	res := c.do(t, "DELETE", "/terminals/"+id, "")
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("닫기 실패: %d", res.StatusCode)
	}
}

// stream은 EventSource가 하는 일을 흉내낸다.
// 토큰을 쿼리로 넘기는 것까지 같다.
func (c *client) stream(t *testing.T, id string, out chan<- string) func() {
	t.Helper()

	res, err := http.Get(c.base + "/terminals/" + id + "/stream?token=" + c.key)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("스트림 실패: %d", res.StatusCode)
	}

	go func() {
		defer res.Body.Close()

		buf := make([]byte, 32*1024)
		var acc strings.Builder
		for {
			n, err := res.Body.Read(buf)
			if n > 0 {
				acc.Write(buf[:n])
				for {
					s := acc.String()
					i := strings.Index(s, "\n\n")
					if i < 0 {
						break
					}
					block := s[:i]
					acc.Reset()
					acc.WriteString(s[i+2:])

					for _, line := range strings.Split(block, "\n") {
						data, ok := strings.CutPrefix(line, "data: ")
						if !ok || strings.HasPrefix(data, "{") {
							continue
						}
						b, e := base64.StdEncoding.DecodeString(data)
						if e != nil {
							continue
						}
						select {
						case out <- string(b):
						default:
						}
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return func() { _ = res.Body.Close() }
}

var ansiRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b\[[0-9;?]*[a-zA-Z]`)

func collectSSE(t *testing.T, ch <-chan string, want string, wait time.Duration) string {
	t.Helper()

	var sb strings.Builder
	deadline := time.After(wait)
	for {
		select {
		case s := <-ch:
			sb.WriteString(s)
			clean := ansiRe.ReplaceAllString(sb.String(), "")
			if strings.Contains(clean, want) {
				return clean
			}
		case <-deadline:
			return ansiRe.ReplaceAllString(sb.String(), "")
		}
	}
}
