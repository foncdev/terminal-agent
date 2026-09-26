// Package config는 환경변수에서 설정을 읽는다.
//
// .env 파일도 읽지만, 셸에 이미 있는 값이 우선한다.
package config

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host string
	Port int

	// APIKey가 비어 있으면 인증을 걸지 않는다. 로컬 전용일 때만 그렇게 쓴다.
	// 비어 있으면 이 기기 주소로 온 요청만 받고, 밖으로 열면 시작하지 않는다.
	APIKey string

	// CORSOrigins는 브라우저에서 직접 부를 수 있는 오리진이다. 기본은 없다.
	//
	// 웹·안경은 relay-service를 거쳐 오고, relaylink는 Origin을 싣지 않는다.
	// 예전에는 어느 오리진이든 되비춰, 키가 없으면 사용자가 연 아무
	// 웹페이지나 이 기기에서 명령을 돌릴 수 있었다.
	CORSOrigins []string

	// AllowedRoots는 터미널을 띄울 수 있는 디렉터리다.
	//
	// 주의: 이것은 시작 위치만 제한한다. 셸 안에서 cd로 나가는 것은
	// 막지 않는다(막을 방법도 마땅치 않다). 실수 방지용이지 보안 경계가 아니다.
	AllowedRoots []string

	// Shell이 비어 있으면 플랫폼 기본값을 쓴다.
	Shell string

	MaxTerminals int
	IdleTimeout  time.Duration

	// Scrollback은 터미널마다 메모리에 들고 있을 출력 바이트 수다.
	// 나중에 붙는 클라이언트에게 화면을 복원해주는 용도다.
	Scrollback int

	// --- 중계 서버 연결 (선택) ---

	// RelayURL이 비어 있으면 붙지 않고 자기 포트로만 연다.
	RelayURL   string
	RelayToken string
	// RelayName은 서버 목록에 보일 이름이다. 비우면 호스트명.
	RelayName string
}

func Load() Config {
	loadDotEnv(".env")

	return Config{
		Host:         str("TERMINAL_HOST", "127.0.0.1"),
		Port:         num("TERMINAL_PORT", 4200),
		APIKey:       str("TERMINAL_API_KEY", ""),
		CORSOrigins:  list(str("TERMINAL_CORS_ORIGINS", "")),
		AllowedRoots: roots(str("TERMINAL_ALLOWED_ROOTS", defaultRoot())),
		Shell:        str("TERMINAL_SHELL", ""),
		MaxTerminals: num("TERMINAL_MAX", 3),
		IdleTimeout:  time.Duration(num("TERMINAL_IDLE_MINUTES", 30)) * time.Minute,
		Scrollback:   num("TERMINAL_SCROLLBACK_BYTES", 256*1024),

		RelayURL:   str("RELAY_URL", ""),
		RelayToken: str("RELAY_TERMINAL_TOKEN", ""),
		RelayName:  str("RELAY_AGENT_NAME", hostname()),
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "terminal-agent"
	}
	return h
}

// LocalOnly는 루프백에만 열려 있는지 본다.
// 밖으로 열려 있는데 키가 없으면 시작하지 않는다.
func (c Config) LocalOnly() bool {
	return IsLoopback(c.Host)
}

// IsLoopback은 이 기기 안에서만 닿는 주소인지 본다.
//
// 이름이 127.로 시작하는지만 보면 127.evil.com 같은 이름이 통과한다.
// DNS 리바인딩이 바로 그런 이름을 127.0.0.1로 돌려 쓰므로, 이름은
// localhost만 받고 나머지는 IP로 풀어서 본다.
func IsLoopback(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// list는 쉼표로 나눈 값을 자른다.
func list(raw string) []string {
	var out []string
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// LogPath는 콘솔 화면이 떠 있는 동안 로그를 적을 곳이다.
// 화면을 차지한 상태에서 stdout에 찍으면 화면이 깨진다.
func (c Config) LogPath() string {
	if p := os.Getenv("TERMINAL_LOG"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "terminal-agent.log")
}

func defaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

// roots는 구분자로 나눈다. 윈도우는 경로에 콜론(C:\)이 있어 세미콜론을 쓴다.
func roots(raw string) []string {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}

	var out []string
	for _, p := range strings.Split(raw, sep) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, expandHome(p))
	}
	return out
}

func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~"+string(filepath.Separator)) && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}

func str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func num(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// loadDotEnv는 KEY=VALUE 줄을 읽어 환경에 넣는다.
// 이미 설정된 값은 건드리지 않는다.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
