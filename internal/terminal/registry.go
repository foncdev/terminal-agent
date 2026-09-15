package terminal

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("터미널을 찾을 수 없습니다")
	ErrTooMany  = errors.New("터미널이 너무 많습니다")
)

// Registry는 살아 있는 터미널을 들고 있는다.
//
// 여러 요청이 동시에 들어오므로 전부 뮤텍스로 감싼다.
type Registry struct {
	mu    sync.RWMutex
	items map[string]*Terminal

	max        int
	scrollback int

	// idleTimeout이 0보다 크면 그만큼 조용한 터미널을 정리한다.
	idleTimeout time.Duration

	stop chan struct{}
	once sync.Once
}

type RegistryOptions struct {
	Max         int
	Scrollback  int
	IdleTimeout time.Duration
}

func NewRegistry(o RegistryOptions) *Registry {
	if o.Max <= 0 {
		o.Max = 3
	}
	if o.Scrollback <= 0 {
		o.Scrollback = 256 * 1024
	}

	r := &Registry{
		items:       make(map[string]*Terminal),
		max:         o.Max,
		scrollback:  o.Scrollback,
		idleTimeout: o.IdleTimeout,
		stop:        make(chan struct{}),
	}

	if r.idleTimeout > 0 {
		go r.sweepLoop()
	}
	return r
}

// CreateOptions는 Registry.Create에 넘기는 값이다.
type CreateOptions struct {
	Dir  string
	Argv []string
	Env  []string
	Cols int
	Rows int
}

func (r *Registry) Create(o CreateOptions) (*Terminal, error) {
	r.mu.Lock()
	if len(r.items) >= r.max {
		r.mu.Unlock()
		return nil, ErrTooMany
	}
	r.mu.Unlock()

	t, err := New(Options{
		ID:         uuid.NewString(),
		Dir:        o.Dir,
		Argv:       o.Argv,
		Env:        o.Env,
		Cols:       o.Cols,
		Rows:       o.Rows,
		Scrollback: r.scrollback,
	})
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.items[t.id] = t
	r.mu.Unlock()

	// 끝난 터미널은 잠시 남겨 종료 코드를 볼 수 있게 했다가 치운다.
	go func() {
		<-t.Done()
		time.Sleep(60 * time.Second)
		r.mu.Lock()
		delete(r.items, t.id)
		r.mu.Unlock()
	}()

	return t, nil
}

func (r *Registry) Get(id string) (*Terminal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}

// List는 만든 순서대로 준다. 목록이 매번 뒤바뀌면 쓰기 불편하다.
func (r *Registry) List() []Info {
	r.mu.RLock()
	out := make([]Info, 0, len(r.items))
	for _, t := range r.items {
		out = append(out, t.Info())
	}
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (r *Registry) Close(id string) error {
	r.mu.Lock()
	t, ok := r.items[id]
	if ok {
		delete(r.items, id)
	}
	r.mu.Unlock()

	if !ok {
		return ErrNotFound
	}
	return t.Close()
}

// CloseAll은 종료할 때 부른다.
func (r *Registry) CloseAll() {
	r.once.Do(func() { close(r.stop) })

	r.mu.Lock()
	items := make([]*Terminal, 0, len(r.items))
	for id, t := range r.items {
		items = append(items, t)
		delete(r.items, id)
	}
	r.mu.Unlock()

	for _, t := range items {
		_ = t.Close()
	}
}

// sweepLoop은 오래 조용한 터미널을 정리한다.
// 안 그러면 잊어버린 셸이 계속 쌓인다.
func (r *Registry) sweepLoop() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	for {
		select {
		case <-r.stop:
			return
		case <-tick.C:
			r.sweep()
		}
	}
}

func (r *Registry) sweep() {
	r.mu.Lock()
	var stale []*Terminal
	for id, t := range r.items {
		if t.IdleFor() > r.idleTimeout {
			stale = append(stale, t)
			delete(r.items, id)
		}
	}
	r.mu.Unlock()

	for _, t := range stale {
		_ = t.Close()
	}
}
