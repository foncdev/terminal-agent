// Package api는 HTTP와 WebSocket 표면이다.
package api

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/foncdev/terminal-agent/internal/config"
	"github.com/foncdev/terminal-agent/internal/lang"
	"github.com/foncdev/terminal-agent/internal/terminal"
)

type Server struct {
	cfg  config.Config
	reg  *terminal.Registry
	mux  *http.ServeMux
	logf func(string, ...any)

	// SSE 뷰어와 리사이즈 요청을 이어준다.
	viewers *viewerBinding
}

func NewServer(cfg config.Config, reg *terminal.Registry) *Server {
	s := &Server{
		cfg:     cfg,
		reg:     reg,
		mux:     http.NewServeMux(),
		logf:    log.Printf,
		viewers: newViewerBinding(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)

	s.mux.HandleFunc("POST /terminals", s.handleCreate)
	s.mux.HandleFunc("GET /terminals", s.handleList)
	s.mux.HandleFunc("GET /terminals/{id}", s.handleGet)
	s.mux.HandleFunc("DELETE /terminals/{id}", s.handleDelete)
	s.mux.HandleFunc("POST /terminals/{id}/input", s.handleInput)
	s.mux.HandleFunc("POST /terminals/{id}/resize", s.handleResize)

	// 시스템 상태. 터미널을 만들지 않고도 읽을 수 있다.
	// 안경이 top·ps를 눈으로 훑는 대신 이걸 쓴다.
	s.mux.HandleFunc("GET /sys/summary", s.handleSysSummary)
	s.mux.HandleFunc("GET /sys/procs", s.handleSysProcs)

	// 미리 등록한 명령을 한 번 실행한다. 터미널을 만들지 않는다.
	s.mux.HandleFunc("POST /run", s.handleRun)
	s.mux.HandleFunc("POST /run/inspect", s.handleInspect)

	// 출력은 두 가지로 받을 수 있다.
	//  - WS : 양방향. 입력까지 같이 보낼 수 있어 지연이 낮다.
	//  - SSE: 단방향. 입력은 POST로 따로 보낸다. 중계 서버를 태우기 쉽다.
	s.mux.HandleFunc("GET /terminals/{id}/ws", s.handleWS)
	s.mux.HandleFunc("GET /terminals/{id}/stream", s.handleSSE)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.withOrigin(s.withHost(s.withAuth(s.mux))).ServeHTTP(w, r)
}

// withOrigin은 브라우저에서 온 요청을 허용 목록으로 거른다.
//
// 이 서비스는 셸을 연다. 예전에는 Origin을 그대로 되비춰서, 키가 없으면
// 사용자가 연 아무 웹페이지나 /run이나 터미널 입력으로 명령을 돌릴 수
// 있었다. 정상 호출자(relaylink, 콘솔)는 브라우저가 아니라 Origin을 싣지
// 않는다. 브라우저는 교차 출처 POST와 WebSocket에 늘 Origin을 싣는다.
func (s *Server) withOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !slices.Contains(s.cfg.CORSOrigins, origin) {
			writeError(w, http.StatusForbidden, "origin_not_allowed",
				lang.L("허용되지 않은 오리진입니다 (TERMINAL_CORS_ORIGINS에 추가): ",
					"Origin not allowed (add it to TERMINAL_CORS_ORIGINS): ")+origin)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-api-key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withHost는 키가 없을 때 이 기기 주소로 온 요청만 받는다.
//
// DNS 리바인딩은 공격자 도메인을 127.0.0.1로 바꿔 같은 출처인 척하므로
// Origin 검사를 지난다. 그때 Host는 공격자 도메인으로 남는다.
func (s *Server) withHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey != "" {
			next.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !config.IsLoopback(host) {
			writeError(w, http.StatusForbidden, "host_not_allowed",
				lang.L("TERMINAL_API_KEY 없이는 이 기기 주소로만 접속할 수 있습니다.",
					"Without TERMINAL_API_KEY, only this device's own address is accepted."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth는 키가 설정돼 있을 때만 인증을 건다.
//
// WebSocket과 SSE는 헤더를 못 붙이는 경우가 있어 쿼리도 받는다.
// 브라우저 WebSocket API는 헤더를 아예 지정할 수 없고, EventSource도 마찬가지다.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey == "" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		provided := r.Header.Get("x-api-key")
		if provided == "" {
			if b := r.Header.Get("Authorization"); strings.HasPrefix(b, "Bearer ") {
				provided = strings.TrimPrefix(b, "Bearer ")
			}
		}
		// 스트림 경로에 한해 쿼리를 허용한다.
		if provided == "" && isStreamPath(r.URL.Path) {
			provided = r.URL.Query().Get("token")
			if provided == "" {
				provided = r.URL.Query().Get("apiKey")
			}
		}

		if subtleCompare(provided, s.cfg.APIKey) {
			next.ServeHTTP(w, r)
			return
		}

		writeError(w, http.StatusUnauthorized, "unauthorized", lang.L("x-api-key가 없거나 다릅니다.", "x-api-key is missing or wrong."))
	})
}

func isStreamPath(p string) bool {
	return strings.HasSuffix(p, "/ws") || strings.HasSuffix(p, "/stream")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"service":   "terminal-agent",
		"ptyReady":  ptyAvailable(),
		"terminals": len(s.reg.List()),
	})
}

// --- 응답 헬퍼 ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errBody
	b.Error.Code = code
	b.Error.Message = msg
	writeJSON(w, status, b)
}
