package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/foncdev/terminal-agent/internal/terminal"
)

// 클라이언트와 주고받는 메시지.
//
// 키가 짧은 것은 키 입력마다 오가기 때문이다.
//
//	클라 → 서버 : {"t":"i","d":"ls\r"}      입력
//	             {"t":"r","c":120,"r":40}   리사이즈
//	서버 → 클라 : {"t":"o","d":"<base64>"}  출력
//	             {"t":"x","code":0}         종료
//
// 출력을 base64로 감싸는 이유: PTY는 임의 바이트를 뱉는데, UTF-8 문자
// 중간에서 잘린 조각이 섞이면 JSON 인코딩이 깨진다. 잘린 조각도 그대로
// 실어 보내야 클라이언트가 이어 붙여 복원할 수 있다.
type clientMessage struct {
	T    string `json:"t"`
	D    string `json:"d,omitempty"`
	Cols int    `json:"c,omitempty"`
	Rows int    `json:"r,omitempty"`
}

type serverMessage struct {
	T    string `json:"t"`
	D    string `json:"d,omitempty"`
	Code *int   `json:"code,omitempty"`
}

// handleWS는 양방향 연결이다. 출력과 입력을 한 소켓으로 처리한다.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// 브라우저·안경앱이 어느 출처에서 올지 정해져 있지 않다.
		// 인증은 위의 withAuth가 맡는다.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	// PTY 출력은 커도 되지만, 클라이언트 입력은 커질 이유가 없다.
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	replay := r.URL.Query().Get("replay") != "0"
	out, unsub := t.Subscribe(replay)
	defer unsub()

	// 이 연결을 뷰어 하나로 등록한다. 맥 콘솔과 창 크기가 달라도
	// 서로 덮어쓰지 않고, 서버가 가장 작은 쪽에 맞춘다.
	viewer := t.NewViewer()
	defer t.DropViewer(viewer)

	// 읽기: 클라이언트 입력을 셸로 넣는다.
	go func() {
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var msg clientMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}

			switch msg.T {
			case "i":
				if terminal.IsTerminalReply([]byte(msg.D)) {
					continue
				}
				if err := t.Write([]byte(msg.D)); err != nil {
					return
				}
			case "r":
				_ = t.ResizeViewer(viewer, msg.Cols, msg.Rows)
			}
		}
	}()

	// 쓰기: 셸 출력을 클라이언트로 보낸다.
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case chunk, ok := <-out:
			if !ok {
				// 셸이 끝났다. 종료 코드를 알리고 닫는다.
				code := t.Info().ExitCode
				_ = writeWS(ctx, conn, serverMessage{T: "x", Code: code})
				_ = conn.Close(websocket.StatusNormalClosure, "terminal exited")
				return
			}
			msg := serverMessage{T: "o", D: base64.StdEncoding.EncodeToString(chunk)}
			if err := writeWS(ctx, conn, msg); err != nil {
				return
			}

		case <-ping.C:
			// 중간 장비가 유휴 연결을 끊지 않게 한다.
			if err := conn.Ping(ctx); err != nil {
				return
			}
		}
	}
}

func writeWS(ctx context.Context, conn *websocket.Conn, msg serverMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

// handleSSE는 출력만 흘려보낸다. 입력은 POST /input으로 받는다.
//
// WebSocket을 못 쓰는 환경과, 중계 서버를 태우기 쉬운 경로를 위해 함께 둔다.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_flush", "스트리밍을 지원하지 않습니다.")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	// 앞단에 nginx가 있어도 버퍼링하지 않게 한다.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	replay := r.URL.Query().Get("replay") != "0"
	out, unsub := t.Subscribe(replay)
	defer unsub()

	// SSE는 단방향이라 크기를 여기로 못 받는다. 뷰어만 잡아두고
	// 실제 크기는 POST /resize가 이 스트림의 뷰어에 실어준다.
	viewer := t.NewViewer()
	defer t.DropViewer(viewer)
	s.bindViewer(t.ID(), viewer)
	defer s.unbindViewer(t.ID(), viewer)

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case chunk, ok := <-out:
			if !ok {
				code := 0
				if c := t.Info().ExitCode; c != nil {
					code = *c
				}
				fmt.Fprintf(w, "event: exit\ndata: {\"code\":%d}\n\n", code)
				flusher.Flush()
				return
			}
			// WS와 같은 이유로 base64로 감싼다.
			fmt.Fprintf(w, "event: output\ndata: %s\n\n",
				base64.StdEncoding.EncodeToString(chunk))
			flusher.Flush()

		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
