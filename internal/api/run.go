package api

import (
	"net/http"
	"time"

	"github.com/foncdev/terminal-agent/internal/lang"
	"github.com/foncdev/terminal-agent/internal/runner"
)

/*
 * 명령을 한 번 실행하고 글을 돌려준다.
 *
 * 터미널(PTY)과 다르다. 대화하지 않고 끝나면 출력만 준다. 안경은 입력이
 * 탭·스크롤 네 가지뿐이라 실셸을 쓸 수 없어서, 미리 등록해 둔 명령을
 * 고르는 형태로 대신한다. 등록과 목록은 relay-service가 들고 있고,
 * 여기서는 실행만 한다.
 *
 * 되돌릴 수 없어 보이는 명령은 confirm 없이는 거부한다. 안경에서 탭
 * 한 번에 실행되므로 손이 스쳐도 일이 벌어진다. 다만 셸은 표현이
 * 무한해서 우회하는 길이 늘 있다 — 실수를 줄이는 장치이지 보안
 * 경계가 아니다.
 */

type runRequest struct {
	Command string `json:"command"`
	Dir     string `json:"dir"`
	// 초 단위. 비우면 기본값(20초).
	TimeoutSec int `json:"timeoutSec"`
	// 위험하다고 걸린 명령을 그래도 실행할지.
	Confirm bool `json:"confirm"`
}

type runResponse struct {
	runner.Result
	// 걸린 위험 신호. 실행했든 막았든 함께 준다.
	Risks []runner.Risk `json:"risks,omitempty"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	risks := runner.Inspect(req.Command)

	// 되돌릴 수 없는 일은 확인을 받고서야 한다.
	if !req.Confirm && runner.NeedsConfirm(req.Command) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{
				"code":    "confirm_required",
				"message": lang.L("되돌릴 수 없는 명령입니다. 확인 후 다시 보내세요.", "This command can't be undone. Confirm and send it again."),
			},
			"risks": risks,
		})
		return
	}

	// 시작 디렉터리는 허용된 곳이어야 한다. 셸을 띄울 때와 같은 검사다.
	dir, err := s.cfg.ResolveDir(req.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_dir", err.Error())
		return
	}

	res, err := runner.Run(r.Context(), req.Command, dir, time.Duration(req.TimeoutSec)*time.Second)
	if err != nil {
		writeError(w, http.StatusBadRequest, "run_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, runResponse{Result: res, Risks: risks})
}

// handleInspect는 실행하지 않고 위험 신호만 알려준다.
// 스니펫을 등록할 때 미리 보여주는 데 쓴다.
func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"risks":        runner.Inspect(req.Command),
		"needsConfirm": runner.NeedsConfirm(req.Command),
	})
}
