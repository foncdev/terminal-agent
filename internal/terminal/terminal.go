// Package terminal은 살아 있는 터미널 하나와 그 구독자들을 관리한다.
package terminal

import (
	"context"
	"sync"
	"time"

	"github.com/foncdev/terminal-agent/internal/pty"
)

// 구독자에게 보낼 때 쓰는 채널 버퍼 크기.
//
// 느린 클라이언트 하나 때문에 PTY 전체가 멈추면 안 된다. 버퍼가 차면
// 그 구독자에게는 보내지 않고 버린다. 화면이 좀 튀는 편이,
// 모두가 멈추는 것보다 낫다.
const subscriberBuffer = 64

// readChunk는 PTY에서 한 번에 읽어올 크기다.
const readChunk = 32 * 1024

// ViewerID는 이 터미널을 보고 있는 화면 하나를 가리킨다.
type ViewerID uint64

type size struct{ cols, rows int }

type Status string

const (
	StatusRunning Status = "running"
	StatusExited  Status = "exited"
)

// Info는 바깥에 보여줄 터미널 상태다.
type Info struct {
	ID           string    `json:"id"`
	Dir          string    `json:"dir"`
	Shell        string    `json:"shell"`
	Cols         int       `json:"cols"`
	Rows         int       `json:"rows"`
	Status       Status    `json:"status"`
	PID          int       `json:"pid"`
	ExitCode     *int      `json:"exitCode,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
}

// Terminal은 PTY 하나와 거기 붙은 구독자들이다.
type Terminal struct {
	id    string
	dir   string
	shell string

	p pty.Pty

	mu        sync.RWMutex
	cols      int
	rows      int
	status    Status
	exitCode  *int
	createdAt time.Time
	lastAt    time.Time

	// subs는 구독자별 채널이다. 키는 구독을 끊을 때 쓰는 표식이다.
	subs map[chan []byte]struct{}

	// viewers는 이 터미널을 보는 화면들의 창 크기다.
	//
	// 맥 콘솔과 웹이 같은 셸을 보는데 창 크기가 다르다. 누가 얼마인지
	// 따로 들고 있다가 가장 작은 쪽에 맞춘다 — 큰 쪽에 맞추면
	// 작은 화면에서 줄이 접혀 프롬프트가 엉킨다.
	viewers    map[ViewerID]size
	nextViewer uint64

	// scroll은 최근 출력이다. 나중에 붙는 클라이언트에게 화면을 되살려준다.
	scroll *ringBuffer

	// done은 프로세스가 끝나면 닫힌다.
	done chan struct{}

	closeOnce sync.Once
}

// Options는 터미널을 만들 때의 조건이다.
type Options struct {
	ID         string
	Dir        string
	Argv       []string
	Env        []string
	Cols       int
	Rows       int
	Scrollback int
}

// New는 PTY를 띄우고 읽기 고루틴을 시작한다.
func New(o Options) (*Terminal, error) {
	p, err := pty.Start(pty.Options{
		Argv: o.Argv,
		Dir:  o.Dir,
		Env:  o.Env,
		Cols: o.Cols,
		Rows: o.Rows,
	})
	if err != nil {
		return nil, err
	}

	shell := ""
	if len(o.Argv) > 0 {
		shell = o.Argv[0]
	} else {
		shell = pty.DefaultShell()
	}

	now := time.Now()
	t := &Terminal{
		id:        o.ID,
		dir:       o.Dir,
		shell:     shell,
		p:         p,
		cols:      o.Cols,
		rows:      o.Rows,
		status:    StatusRunning,
		createdAt: now,
		lastAt:    now,
		subs:      make(map[chan []byte]struct{}),
		viewers:   make(map[ViewerID]size),
		scroll:    newRingBuffer(o.Scrollback),
		done:      make(chan struct{}),
	}

	go t.pump()
	go t.reap()

	return t, nil
}

func (t *Terminal) ID() string            { return t.id }
func (t *Terminal) Done() <-chan struct{} { return t.done }

// Subscribers는 지금 이 터미널을 보고 있는 수다.
// 로컬 UI가 "웹 n명"을 띄우는 데 쓴다.
func (t *Terminal) Subscribers() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.subs)
}

// pump은 PTY 출력을 읽어 구독자에게 나눠준다.
//
// 고루틴 하나가 PTY를 독점해서 읽는다. 여러 곳에서 읽으면 바이트가
// 섞이므로 반드시 한 곳이어야 한다.
func (t *Terminal) pump() {
	buf := make([]byte, readChunk)
	for {
		n, err := t.p.Read(buf)
		if n > 0 {
			// 구독자마다 따로 복사한다. 같은 슬라이스를 나눠주면
			// 다음 Read가 덮어써서 내용이 바뀐다.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			t.broadcast(chunk)
		}
		if err != nil {
			// 셸이 끝나면 EOF나 EIO가 온다. 둘 다 정상 종료다.
			return
		}
	}
}

// reap은 프로세스 종료를 기다렸다가 상태를 바꾸고 구독자를 정리한다.
func (t *Terminal) reap() {
	code, err := t.p.Wait(context.Background())

	t.mu.Lock()
	t.status = StatusExited
	if err == nil {
		t.exitCode = &code
	}
	t.mu.Unlock()

	t.closeSubs()
	close(t.done)
}

func (t *Terminal) broadcast(chunk []byte) {
	t.mu.Lock()
	t.lastAt = time.Now()
	t.scroll.write(chunk)
	subs := make([]chan []byte, 0, len(t.subs))
	for ch := range t.subs {
		subs = append(subs, ch)
	}
	t.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- chunk:
		default:
			// 이 구독자가 밀렸다. 버리고 넘어간다.
		}
	}
}

// Subscribe는 출력 채널과 해지 함수를 준다.
//
// replay가 true면 그동안 쌓인 화면을 먼저 한 덩어리로 보낸다.
// 새 창을 열었을 때 빈 화면이 아니라 하던 작업이 보이게 하려는 것이다.
func (t *Terminal) Subscribe(replay bool) (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberBuffer)

	t.mu.Lock()
	if replay {
		if hist := t.scroll.bytes(); len(hist) > 0 {
			// 버퍼가 넉넉하므로 막히지 않는다.
			ch <- hist
		}
	}
	t.subs[ch] = struct{}{}
	t.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			t.mu.Lock()
			if _, ok := t.subs[ch]; ok {
				delete(t.subs, ch)
				close(ch)
			}
			t.mu.Unlock()
		})
	}
	return ch, cancel
}

func (t *Terminal) closeSubs() {
	t.mu.Lock()
	for ch := range t.subs {
		delete(t.subs, ch)
		close(ch)
	}
	t.mu.Unlock()
}

// Write는 키 입력을 셸에 넣는다.
func (t *Terminal) Write(b []byte) error {
	t.mu.Lock()
	t.lastAt = time.Now()
	t.mu.Unlock()

	_, err := t.p.Write(b)
	return err
}

// Resize는 창 크기를 바꾼다.
//
// 뷰어가 하나뿐일 때 쓰는 간편한 형태다. 맥 콘솔과 웹이 같이 보는
// 상황에서는 ResizeViewer를 쓴다 — 서로 크기를 덮어쓰면 화면이 깨진다.
func (t *Terminal) Resize(cols, rows int) error {
	return t.applySize(cols, rows)
}

// NewViewer는 이 터미널을 보는 화면 하나를 등록하고 그 손잡이를 준다.
//
// 뷰어마다 창 크기가 다르므로 누가 얼마인지 따로 들고 있어야 한다.
func (t *Terminal) NewViewer() ViewerID {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextViewer++
	id := ViewerID(t.nextViewer)
	t.viewers[id] = size{}
	return id
}

// DropViewer는 화면이 떠났음을 알린다. 남은 뷰어에 맞춰 크기를 다시 정한다.
func (t *Terminal) DropViewer(id ViewerID) {
	t.mu.Lock()
	delete(t.viewers, id)
	c, r := t.smallestLocked()
	t.mu.Unlock()

	if c > 0 && r > 0 {
		_ = t.applySize(c, r)
	}
}

// ResizeViewer는 뷰어 하나의 창 크기를 알린다.
//
// 실제 셸 크기는 **가장 작은 뷰어**에 맞춘다. tmux가 같은 방식이다.
// 큰 쪽에 맞추면 작은 화면에서 줄이 접히고 프롬프트가 엉킨다.
func (t *Terminal) ResizeViewer(id ViewerID, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}

	t.mu.Lock()
	if _, ok := t.viewers[id]; !ok {
		// 모르는 뷰어면 그냥 등록해둔다. 화면이 안 보이는 것보다 낫다.
		t.viewers[id] = size{}
	}
	t.viewers[id] = size{cols: cols, rows: rows}
	c, r := t.smallestLocked()
	t.mu.Unlock()

	if c <= 0 || r <= 0 {
		return nil
	}
	return t.applySize(c, r)
}

// smallestLocked는 크기를 알린 뷰어들의 차원별 최솟값이다.
// 호출 전에 뮤텍스를 잡아야 한다.
func (t *Terminal) smallestLocked() (int, int) {
	c, r := 0, 0
	for _, s := range t.viewers {
		// 크기를 아직 안 알린 뷰어는 뺀다. 넣으면 0이 최솟값이 된다.
		if s.cols <= 0 || s.rows <= 0 {
			continue
		}
		if c == 0 || s.cols < c {
			c = s.cols
		}
		if r == 0 || s.rows < r {
			r = s.rows
		}
	}
	return c, r
}

func (t *Terminal) applySize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}

	t.mu.RLock()
	same := t.cols == cols && t.rows == rows
	t.mu.RUnlock()
	if same {
		// 같은 크기로 또 부르면 셸에 SIGWINCH만 쌓인다.
		return nil
	}

	if err := t.p.Resize(cols, rows); err != nil {
		return err
	}

	t.mu.Lock()
	t.cols, t.rows = cols, rows
	t.lastAt = time.Now()
	t.mu.Unlock()
	return nil
}

// Close는 셸을 끝낸다. 여러 번 불러도 안전하다.
func (t *Terminal) Close() error {
	var err error
	t.closeOnce.Do(func() { err = t.p.Close() })
	return err
}

// IdleFor는 마지막 활동 이후 지난 시간이다.
func (t *Terminal) IdleFor() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return time.Since(t.lastAt)
}

func (t *Terminal) Info() Info {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return Info{
		ID:           t.id,
		Dir:          t.dir,
		Shell:        t.shell,
		Cols:         t.cols,
		Rows:         t.rows,
		Status:       t.status,
		PID:          t.p.Pid(),
		ExitCode:     t.exitCode,
		CreatedAt:    t.createdAt,
		LastActiveAt: t.lastAt,
	}
}
