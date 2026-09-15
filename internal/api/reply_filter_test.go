package api_test

import (
	"encoding/json"
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

// 웹으로 들어온 터미널 응답도 셸에 전달되지 않아야 한다.
//
// 브라우저가 붙어 있는 상태에서 바깥 터미널 응답이 올라오면,
// 맥 콘솔과 똑같이 "1;2c"가 찍힌다. 경로가 다를 뿐 같은 문제다.
func TestWebInputFiltersTerminalReplies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	dir := t.TempDir()
	reg := terminal.NewRegistry(terminal.RegistryOptions{Max: 2, Scrollback: 64 * 1024})
	defer reg.CloseAll()

	srv := httptest.NewServer(api.NewServer(
		config.Config{AllowedRoots: []string{dir}, APIKey: "k", MaxTerminals: 2}, reg))
	defer srv.Close()

	// 터미널 생성
	id := ""
	{
		body := `{"dir":"` + dir + `","argv":["/bin/sh"],"cols":80,"rows":24}`
		req, _ := http.NewRequest("POST", srv.URL+"/terminals", strings.NewReader(body))
		req.Header.Set("x-api-key", "k")
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		var out struct {
			Terminal terminal.Info `json:"terminal"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		id = out.Terminal.ID
	}

	term, err := reg.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	out, unsub := term.Subscribe(false)
	defer unsub()
	time.Sleep(400 * time.Millisecond)

	send := func(data string) {
		b, _ := json.Marshal(map[string]string{"data": data})
		req, _ := http.NewRequest("POST", srv.URL+"/terminals/"+id+"/input", strings.NewReader(string(b)))
		req.Header.Set("x-api-key", "k")
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		_ = res.Body.Close()
	}

	// 추적에서 나온 실제 바이트. 이게 셸에 가면 안 된다.
	send("\x1b[O\x1b[?1;2c\x1b[O\x1b[?1;2c")
	time.Sleep(500 * time.Millisecond)

	// 정상 입력은 동작해야 한다.
	send("echo W\"EB\"OK\n")

	var sb strings.Builder
	deadline := time.After(5 * time.Second)
collect:
	for {
		select {
		case b, ok := <-out:
			if !ok {
				break collect
			}
			sb.WriteString(string(b))
			if strings.Contains(sb.String(), "WEBOK") {
				time.Sleep(600 * time.Millisecond)
				break collect
			}
		case <-deadline:
			break collect
		}
	}

	clean := regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b\[[0-9;?]*[a-zA-Z]`).
		ReplaceAllString(sb.String(), "")

	if strings.Contains(clean, "1;2c") {
		t.Fatalf("응답이 셸로 샜다:\n%s", clean)
	}
	if !strings.Contains(clean, "WEBOK") {
		t.Fatalf("정상 입력이 동작하지 않는다 — 필터가 과하다:\n%s", clean)
	}
}
