package api

import (
	"net/http"
	"strconv"

	"github.com/foncdev/terminal-agent/internal/lang"
	"github.com/foncdev/terminal-agent/internal/sysinfo"
)

/*
 * 시스템 상태를 숫자로 준다.
 *
 * top·ps를 셸로 돌려 화면에 흘리는 대신 여기서 한 번 읽어 값만 넘긴다.
 * 안경은 576×288 단색에 64칸이라 ANSI 이스케이프가 그대로 오면 깨지고,
 * top은 갱신을 계속 밀어 배터리를 먹는다.
 *
 * PTY를 쓰지 않으므로 터미널을 만들지 않아도 된다. 셸도 열지 않는다 —
 * 명령과 인자를 코드에 박아 두어 사용자 입력이 섞이지 않는다.
 */

func (s *Server) handleSysSummary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sysinfo.Get(r.Context()))
}

func (s *Server) handleSysProcs(w http.ResponseWriter, r *http.Request) {
	// 기본 10개. 안경 한 화면에 들어가는 수다.
	n := 10
	if raw := r.URL.Query().Get("n"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", lang.L("n은 숫자여야 합니다.", "n must be a number."))
			return
		}
		n = parsed
	}

	procs, err := sysinfo.Procs(r.Context(), n)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sysinfo_failed",
			lang.L("프로세스 목록을 읽지 못했습니다: ", "Couldn't read the process list: ")+err.Error())
		return
	}
	// nil로 두면 JSON에 null이 나가 받는 쪽이 길이를 못 센다.
	if procs == nil {
		procs = []sysinfo.Proc{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"procs": procs})
}
