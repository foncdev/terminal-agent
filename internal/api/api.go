// Package api는 HTTP와 WebSocket 표면이다.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/foncdev/terminal-agent/internal/config"
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

	// 출력은 두 가지로 받을 수 있다.
	//  - WS : 양방향. 입력까지 같이 보낼 수 있어 지연이 낮다.
	//  - SSE: 단방향. 입력은 POST로 따로 보낸다. 중계 서버를 태우기 쉽다.
	s.mux.HandleFunc("GET /terminals/{id}/ws", s.handleWS)
	s.mux.HandleFunc("GET /terminals/{id}/stream", s.handleSSE)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.withCORS(s.withAuth(s.mux)).ServeHTTP(w, r)
}

// withCORS는 브라우저에서 바로 붙을 수 있게 한다.
// 안경앱이 file://에서 도는 경우가 있어 Origin을 그대로 되비춘다.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-api-key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
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

		writeError(w, http.StatusUnauthorized, "unauthorized", "x-api-key가 없거나 다릅니다.")
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
