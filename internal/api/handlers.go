package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/foncdev/terminal-agent/internal/pty"
	"github.com/foncdev/terminal-agent/internal/terminal"
)

func ptyAvailable() bool { return pty.Available() }

// subtleCompare는 길이를 흘리지 않게 상수 시간으로 비교한다.
func subtleCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type createRequest struct {
	Dir  string   `json:"dir"`
	Argv []string `json:"argv"`
	Cols int      `json:"cols"`
	Rows int      `json:"rows"`
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if !ptyAvailable() {
		writeError(w, http.StatusNotImplemented, "pty_unsupported",
			"이 시스템에서는 터미널을 쓸 수 없습니다. 윈도우는 10 1809 이상이 필요합니다.")
		return
	}

	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	dir, err := s.cfg.ResolveDir(req.Dir)
	if err != nil {
		writeError(w, http.StatusForbidden, "path_not_allowed", err.Error())
		return
	}

	argv, err := s.resolveArgv(req.Argv)
	if err != nil {
		writeError(w, http.StatusForbidden, "command_not_allowed", err.Error())
		return
	}

	t, err := s.reg.Create(terminal.CreateOptions{
		Dir:  dir,
		Argv: argv,
		Cols: req.Cols,
		Rows: req.Rows,
	})
	switch {
	case errors.Is(err, terminal.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, "too_many",
			"터미널이 너무 많습니다. 쓰지 않는 것을 닫아주세요.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "spawn_failed", err.Error())
		return
	}

	// 셸을 열었다는 사실은 남긴다. 입력 내용은 남기지 않는다 —
	// 비밀번호가 섞이기 때문이다.
	s.logf("[terminal] 생성 %s dir=%s shell=%s", t.ID(), dir, t.Info().Shell)

	writeJSON(w, http.StatusCreated, map[string]any{"terminal": t.Info()})
}

// resolveArgv는 실행할 명령을 정한다.
//
// 비어 있으면 설정된 셸을 쓴다. 윈도우에서는 배치 파일과 cmd.exe를 막는다 —
// 인용 규칙이 달라 인자에 섞인 특수문자가 빠져나갈 수 있기 때문이다.
func (s *Server) resolveArgv(argv []string) ([]string, error) {
	if len(argv) == 0 {
		if s.cfg.Shell != "" {
			return []string{s.cfg.Shell}, nil
		}
		return nil, nil // pty가 기본 셸을 고른다
	}

	if runtime.GOOS == "windows" {
		name := strings.ToLower(filepath.Base(argv[0]))
		ext := strings.ToLower(filepath.Ext(argv[0]))
		if ext == ".bat" || ext == ".cmd" || name == "cmd.exe" || name == "cmd" {
			return nil, errors.New("윈도우에서는 배치 파일과 cmd.exe를 직접 실행할 수 없습니다")
		}
	}
	return argv, nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"terminals": s.reg.List()})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"terminal": t.Info()})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.reg.Close(id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	s.logf("[terminal] 종료 %s", id)
	w.WriteHeader(http.StatusNoContent)
}

type inputRequest struct {
	Data string `json:"data"`
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}

	var req inputRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	// 브라우저가 터미널 응답을 그대로 올려보내는 경우가 있다. 걸러낸다.
	if terminal.IsTerminalReply([]byte(req.Data)) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	if err := t.Write([]byte(req.Data)); err != nil {
		writeError(w, http.StatusBadGateway, "write_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type resizeRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (s *Server) handleResize(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}

	var req resizeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "cols와 rows는 1 이상이어야 합니다.")
		return
	}

	// 이 크기가 "어느 화면의 것"인지 붙여준다. 열린 SSE 스트림이 있으면
	// 그 뷰어의 크기로 기록하고, 서버는 모든 뷰어 중 가장 작은 값에 맞춘다.
	// 맥 콘솔이 같은 셸을 보고 있어도 서로 덮어쓰지 않는다.
	var err error
	if v, ok := s.viewers.latest(t.ID()); ok {
		err = t.ResizeViewer(v, req.Cols, req.Rows)
	} else {
		// 스트림 없이 크기만 보낸 경우(테스트 등)는 그대로 적용한다.
		err = t.Resize(req.Cols, req.Rows)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "resize_failed", err.Error())
		return
	}

	// 실제로 정해진 크기를 돌려준다.
	//
	// 뷰어가 여럿이면 가장 작은 쪽에 맞추므로, 요청한 값과 다를 수 있다.
	// 클라이언트는 이 값으로 자기 화면을 맞춰야 vi 같은 전체화면 앱의
	// 마지막 줄이 엉뚱한 자리에 찍히지 않는다.
	info := t.Info()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"cols": info.Cols,
		"rows": info.Rows,
	})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*terminal.Terminal, bool) {
	t, err := s.reg.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "터미널을 찾을 수 없습니다.")
		return nil, false
	}
	return t, true
}

// decodeJSON은 본문을 읽는다. 빈 본문은 기본값으로 본다.
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		// 본문이 아예 없으면 그냥 기본값으로 둔다.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
