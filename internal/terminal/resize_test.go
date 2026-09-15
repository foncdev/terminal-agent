package terminal

import (
	"runtime"
	"testing"
)

// 뷰어가 둘 이상일 때 크기를 어떻게 정할지.
//
// 맥 콘솔과 웹이 같은 셸을 본다. 창 크기가 다른데 마지막 호출자가
// 이기면, 큰 쪽 크기로 설정된 셸의 출력이 작은 쪽 화면을 넘어가
// 줄이 접히고 프롬프트가 엉킨다. 실제로 겪은 증상이다.
//
// tmux와 같은 방식으로 간다: 가장 작은 뷰어에 맞춘다.
// 그래야 모두의 화면에 내용이 들어간다.
func TestResizeUsesSmallestViewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	term := newTestTerm(t)

	// 맥 콘솔이 붙는다.
	mac := term.NewViewer()
	if err := term.ResizeViewer(mac, 202, 52); err != nil {
		t.Fatal(err)
	}
	if got := term.Info(); got.Cols != 202 || got.Rows != 52 {
		t.Fatalf("뷰어 하나뿐일 때는 그 크기여야 한다: %dx%d", got.Cols, got.Rows)
	}

	// 웹이 더 큰 창으로 붙는다. 큰 쪽에 맞추면 맥 화면이 깨진다.
	web := term.NewViewer()
	if err := term.ResizeViewer(web, 339, 75); err != nil {
		t.Fatal(err)
	}
	if got := term.Info(); got.Cols != 202 || got.Rows != 52 {
		t.Fatalf("작은 쪽(202x52)에 맞춰야 한다: %dx%d", got.Cols, got.Rows)
	}

	// 웹이 더 작아지면 이제 웹이 기준이 된다.
	if err := term.ResizeViewer(web, 100, 30); err != nil {
		t.Fatal(err)
	}
	if got := term.Info(); got.Cols != 100 || got.Rows != 30 {
		t.Fatalf("더 작아진 웹에 맞춰야 한다: %dx%d", got.Cols, got.Rows)
	}

	// 웹이 나가면 맥 크기로 돌아와야 한다.
	term.DropViewer(web)
	if got := term.Info(); got.Cols != 202 || got.Rows != 52 {
		t.Fatalf("웹이 나가면 맥 크기로 돌아와야 한다: %dx%d", got.Cols, got.Rows)
	}
}

// 차원별로 따로 고른다. 가로는 A가 작고 세로는 B가 작을 수 있다.
func TestResizeMixedDimensions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	term := newTestTerm(t)

	a := term.NewViewer()
	b := term.NewViewer()

	_ = term.ResizeViewer(a, 100, 60) // 가로가 좁다
	_ = term.ResizeViewer(b, 200, 30) // 세로가 짧다

	got := term.Info()
	if got.Cols != 100 || got.Rows != 30 {
		t.Fatalf("각 차원의 최솟값이어야 한다: got %dx%d, want 100x30", got.Cols, got.Rows)
	}
}

// 크기를 알리지 않은 뷰어는 계산에서 빠져야 한다.
// 안 그러면 0이 최솟값이 되어 셸이 망가진다.
func TestResizeIgnoresUnsizedViewers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 실기기에서 확인한다")
	}

	term := newTestTerm(t)

	a := term.NewViewer()
	_ = term.ResizeViewer(a, 120, 40)

	// 붙기만 하고 크기를 안 알린 뷰어.
	_ = term.NewViewer()

	if got := term.Info(); got.Cols != 120 || got.Rows != 40 {
		t.Fatalf("크기 없는 뷰어는 무시해야 한다: %dx%d", got.Cols, got.Rows)
	}
}

func newTestTerm(t *testing.T) *Terminal {
	t.Helper()

	reg := NewRegistry(RegistryOptions{Max: 2, Scrollback: 8 * 1024})
	t.Cleanup(reg.CloseAll)

	term, err := reg.Create(CreateOptions{
		Dir:  t.TempDir(),
		Argv: []string{"/bin/sh"},
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	return term
}
