// Package relaylink은 중계 서버로 나가는 연결이다.
//
// 이 기기가 공유기 안에 있어도 서버가 먼저 닿을 필요가 없도록, 이쪽에서 붙는다.
// 서버가 보내온 요청을 자기 HTTP API로 대신 호출해 결과를 돌려준다.
//
// 프로토콜:
//
//	서버 → agent : welcome / request / stream_open / stream_close
//	agent → 서버 : response / error / stream_chunk
package relaylink

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	retryMin = 2 * time.Second
	retryMax = 60 * time.Second

	// 서버가 죽은 걸 알아채는 시간. 서버도 30초마다 핑을 보낸다.
	readTimeout = 90 * time.Second
)

// serverMessage는 서버가 보내오는 것이다.
type serverMessage struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Body    string `json:"body"`
	AgentID string `json:"agentId"`
}

// Status는 바깥(로컬 UI)에 보여줄 연결 상태다.
type Status struct {
	Connected bool
	AgentID   string
	URL       string
	LastError string
	Since     time.Time
}

type Link struct {
	url    string
	token  string
	name   string
	local  string // 자기 HTTP API 주소
	apiKey string

	conn   *websocket.Conn
	connMu sync.Mutex

	// streams는 열려 있는 SSE 구독이다. 서버가 닫으라면 취소한다.
	streams   map[string]context.CancelFunc
	streamsMu sync.Mutex

	status   Status
	statusMu sync.RWMutex

	// onChange는 상태가 바뀔 때 불린다. UI가 다시 그리는 데 쓴다.
	onChange func(Status)

	logf func(string, ...any)
}

type Options struct {
	URL      string // ws://host:4100/terminal-agent
	Token    string
	Name     string
	LocalURL string // http://127.0.0.1:4200
	APIKey   string
	OnChange func(Status)
	Logf     func(string, ...any)
}

func New(o Options) *Link {
	l := &Link{
		url:      o.URL,
		token:    o.Token,
		name:     o.Name,
		local:    o.LocalURL,
		apiKey:   o.APIKey,
		streams:  make(map[string]context.CancelFunc),
		onChange: o.OnChange,
		logf:     o.Logf,
	}
	if l.logf == nil {
		l.logf = log.Printf
	}
	l.status.URL = o.URL
	return l
}

func (l *Link) Status() Status {
	l.statusMu.RLock()
	defer l.statusMu.RUnlock()
	return l.status
}

func (l *Link) setStatus(f func(*Status)) {
	l.statusMu.Lock()
	f(&l.status)
	s := l.status
	l.statusMu.Unlock()

	if l.onChange != nil {
		l.onChange(s)
	}
}

// Run은 끊기면 다시 붙기를 반복한다. ctx가 끝나면 돌아온다.
func (l *Link) Run(ctx context.Context) {
	backoff := retryMin

	for {
		if ctx.Err() != nil {
			return
		}

		err := l.connect(ctx)

		if ctx.Err() != nil {
			return
		}

		l.setStatus(func(s *Status) {
			s.Connected = false
			s.AgentID = ""
			if err != nil {
				s.LastError = err.Error()
			}
		})

		l.logf("[relay] 끊김. %.0f초 후 재시도", backoff.Seconds())

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// 계속 실패하면 간격을 늘린다. 서버를 두드리지 않는다.
		backoff *= 2
		if backoff > retryMax {
			backoff = retryMax
		}
	}
}

func (l *Link) connect(ctx context.Context) error {
	target, err := url.Parse(l.url)
	if err != nil {
		return fmt.Errorf("relay 주소가 잘못됐습니다: %w", err)
	}
	q := target.Query()
	q.Set("name", l.name)
	if l.token != "" {
		q.Set("token", l.token)
	}
	target.RawQuery = q.Encode()

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, target.String(), nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	// 터미널 출력이 한꺼번에 몰릴 수 있다.
	conn.SetReadLimit(8 << 20)

	l.connMu.Lock()
	l.conn = conn
	l.connMu.Unlock()

	l.logf("[relay] 연결됨: %s", l.url)
	l.setStatus(func(s *Status) {
		s.Connected = true
		s.LastError = ""
		s.Since = time.Now()
	})

	defer l.closeAllStreams()

	for {
		readCtx, readCancel := context.WithTimeout(ctx, readTimeout)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			return err
		}

		var msg serverMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		l.handle(ctx, msg)
	}
}

func (l *Link) handle(ctx context.Context, msg serverMessage) {
	switch msg.Type {
	case "welcome":
		l.logf("[relay] 등록 완료 (%s)", short(msg.AgentID))
		l.setStatus(func(s *Status) { s.AgentID = msg.AgentID })

	case "request":
		if msg.ID == "" || msg.Method == "" || msg.Path == "" {
			return
		}
		go l.proxyRequest(ctx, msg)

	case "stream_open":
		if msg.ID == "" || msg.Path == "" {
			return
		}
		go l.openStream(ctx, msg.ID, msg.Path)

	case "stream_close":
		l.streamsMu.Lock()
		if cancel, ok := l.streams[msg.ID]; ok {
			cancel()
			delete(l.streams, msg.ID)
		}
		l.streamsMu.Unlock()
	}
}

// proxyRequest는 서버가 넘긴 요청을 자기 API로 호출하고 결과를 돌려준다.
func (l *Link) proxyRequest(ctx context.Context, msg serverMessage) {
	reqCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	var body io.Reader
	if msg.Body != "" {
		body = strings.NewReader(msg.Body)
	}

	req, err := http.NewRequestWithContext(reqCtx, msg.Method, l.local+msg.Path, body)
	if err != nil {
		l.send(map[string]any{"type": "error", "id": msg.ID, "message": err.Error()})
		return
	}
	l.addAuth(req)
	if msg.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		l.send(map[string]any{"type": "error", "id": msg.ID, "message": err.Error()})
		return
	}
	defer res.Body.Close()

	out, err := io.ReadAll(res.Body)
	if err != nil {
		l.send(map[string]any{"type": "error", "id": msg.ID, "message": err.Error()})
		return
	}

	l.send(map[string]any{
		"type":   "response",
		"id":     msg.ID,
		"status": res.StatusCode,
		"body":   string(out),
	})
}

// openStream은 자기 SSE를 열어 오는 대로 서버에 넘긴다.
func (l *Link) openStream(ctx context.Context, id, path string) {
	streamCtx, cancel := context.WithCancel(ctx)

	l.streamsMu.Lock()
	l.streams[id] = cancel
	l.streamsMu.Unlock()

	defer func() {
		cancel()
		l.streamsMu.Lock()
		delete(l.streams, id)
		l.streamsMu.Unlock()
		l.send(map[string]any{"type": "stream_chunk", "id": id, "chunk": "", "done": true})
	}()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, l.local+path, nil)
	if err != nil {
		return
	}
	l.addAuth(req)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()

	// SSE는 줄 단위다. 오는 대로 그대로 넘긴다 — 파싱은 최종 클라이언트가 한다.
	r := bufio.NewReaderSize(res.Body, 64*1024)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			l.send(map[string]any{
				"type":  "stream_chunk",
				"id":    id,
				"chunk": string(buf[:n]),
				"done":  false,
			})
		}
		if err != nil {
			return
		}
	}
}

func (l *Link) addAuth(req *http.Request) {
	if l.apiKey != "" {
		req.Header.Set("x-api-key", l.apiKey)
	}
}

func (l *Link) send(payload any) {
	l.connMu.Lock()
	conn := l.conn
	l.connMu.Unlock()
	if conn == nil {
		return
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 끊긴 연결은 Run의 재접속이 정리한다. 여기서는 조용히 버린다.
	_ = conn.Write(ctx, websocket.MessageText, b)
}

func (l *Link) closeAllStreams() {
	l.streamsMu.Lock()
	for id, cancel := range l.streams {
		cancel()
		delete(l.streams, id)
	}
	l.streamsMu.Unlock()
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
