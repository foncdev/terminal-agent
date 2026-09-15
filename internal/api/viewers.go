package api

import (
	"sync"

	"github.com/foncdev/terminal-agent/internal/terminal"
)

// SSE는 단방향이라 클라이언트가 자기 창 크기를 그 연결로 못 보낸다.
// 크기는 POST /terminals/{id}/resize 로 따로 온다.
//
// 그 요청이 "어느 화면의 크기"인지 알아야 뷰어별로 관리할 수 있다.
// 지금은 터미널마다 SSE 뷰어를 하나로 보고, 마지막에 열린 스트림에
// 크기를 실어준다. 브라우저 탭을 둘 열면 나중 것이 기준이 된다 —
// WebSocket(/ws)을 쓰면 연결마다 정확히 갈린다.
type viewerBinding struct {
	mu   sync.Mutex
	byID map[string][]terminal.ViewerID
}

func newViewerBinding() *viewerBinding {
	return &viewerBinding{byID: make(map[string][]terminal.ViewerID)}
}

func (b *viewerBinding) bind(termID string, v terminal.ViewerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.byID[termID] = append(b.byID[termID], v)
}

func (b *viewerBinding) unbind(termID string, v terminal.ViewerID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	list := b.byID[termID]
	for i, x := range list {
		if x == v {
			b.byID[termID] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(b.byID[termID]) == 0 {
		delete(b.byID, termID)
	}
}

// latest는 이 터미널에 마지막으로 붙은 SSE 뷰어다.
// 없으면 false.
func (b *viewerBinding) latest(termID string) (terminal.ViewerID, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	list := b.byID[termID]
	if len(list) == 0 {
		return 0, false
	}
	return list[len(list)-1], true
}

func (s *Server) bindViewer(termID string, v terminal.ViewerID) {
	s.viewers.bind(termID, v)
}

func (s *Server) unbindViewer(termID string, v terminal.ViewerID) {
	s.viewers.unbind(termID, v)
}
