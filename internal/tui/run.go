package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Run은 로컬 화면을 띄우고 끝날 때까지 기다린다.
//
// 화면이 닫히면(사용자가 나가거나 셸이 끝나면) 돌아온다.
// 호출자는 그때 프로세스를 정리한다.
//
// 반환된 Model로 relay 상태를 밀어넣을 수 있다 — Run이 블로킹이므로
// 시작 전에 Model을 받아야 한다면 New를 직접 쓴다.
func Run(ctx context.Context, m *Model) error {
	if m == nil {
		return fmt.Errorf("모델이 없습니다")
	}
	defer m.Close()

	// 시그널은 main이 ctx로 이미 받고 있다. Bubble Tea가 따로 또 받으면
	// 누가 먼저 잡느냐에 따라 종료가 들쭉날쭉해진다(kill이 안 먹는 경우가
	// 생긴다). 한쪽만 담당하게 해서 ctx 취소 하나로 통일한다.
	//
	// raw 모드에서 ctrl+c는 시그널이 아니라 키 입력으로 오므로,
	// 이렇게 해도 셸로 전달되는 데는 지장이 없다.
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)
	_, err := p.Run()
	return err
}

// 컴파일 시점에 Model이 tea.Model을 만족하는지 확인한다.
var _ tea.Model = (*Model)(nil)
