// Package tui는 맥에서 직접 뜨는 콘솔 화면이다.
//
// 상단 한 줄에 연결 상태를 고정하고, 그 아래로 실제 셸이 돈다.
// 같은 셸이 웹에도 동시에 보인다 — 화면을 공유하는 셈이다.
//
// 어떻게 되는가:
//
//	PTY ──▶ 에뮬레이터(화면 격자) ──Render()──▶ Bubble Tea View
//	    └──▶ 웹 구독자들
//
//	키 입력 ──keyToBytes──▶ Terminal.Write ──▶ PTY
//
// 왜 에뮬레이터를 거치는가: Bubble Tea가 화면을 셀 단위로 관리하는데,
// 자식이 뱉는 이스케이프가 같은 곳으로 나가면 서로 화면을 망친다.
// vim이 1행에 그리려 하면 상태바를 덮어버린다. 그래서 자식의 출력을
// 우리가 먼저 해석해 격자로 만들고, 그 격자만 Bubble Tea에 넘긴다.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"

	"github.com/foncdev/terminal-agent/internal/relaylink"
	"github.com/foncdev/terminal-agent/internal/terminal"
)

// StatusHeight는 상태바가 차지하는 줄 수다. main이 초기 크기를 정할 때 쓴다.
const StatusHeight = statusHeight

// statusHeight는 상태바가 차지하는 줄 수다.
// 셸에게는 이만큼 뺀 높이를 줘야 화면이 어긋나지 않는다.
const statusHeight = 1

// 나가는 키. Ctrl+C는 셸로 보내야 하므로 쓸 수 없다.
const quitKey = "ctrl+\\"

// --- 메시지 ---

// dirtyMsg는 화면이 바뀌었으니 다시 그리라는 신호다.
// 내용은 이미 에뮬레이터에 들어가 있으므로 신호만 보낸다.
type dirtyMsg struct{}

// exitedMsg는 셸이 끝났을 때 온다.
type exitedMsg struct{}

// statusMsg는 relay 연결 상태가 바뀌었을 때 온다.
type statusMsg relaylink.Status

// tickMsg는 상태바의 시간 표시를 갱신한다.
type tickMsg time.Time

// Model은 화면 상태다.
type Model struct {
	term *terminal.Terminal
	emu  *vt.SafeEmulator

	// dirty는 다시 그릴 때가 됐음을 알린다. 버퍼 1이라 여러 번 와도 한 번만 쌓인다.
	dirty chan struct{}

	// out은 터미널 구독 채널이다. 로컬 화면도 웹과 같은 자격의 구독자다.
	out   <-chan []byte
	unsub func()

	// viewer는 이 화면의 크기를 등록해두는 손잡이다.
	// 웹과 창 크기가 달라도 서로 덮어쓰지 않게 한다.
	viewer terminal.ViewerID
	status relaylink.Status

	width  int
	height int

	agentName string
	relayOn   bool
	started   time.Time

	quitting bool
	exited   bool
}

// Options는 화면을 만들 때 필요한 것들이다.
type Options struct {
	Terminal  *terminal.Terminal
	AgentName string
	// RelayEnabled가 false면 상태바에 relay 칸을 아예 안 그린다.
	RelayEnabled bool
}

func New(o Options) *Model {
	cols, rows := o.Terminal.Info().Cols, o.Terminal.Info().Rows

	m := &Model{
		term:      o.Terminal,
		emu:       vt.NewSafeEmulator(cols, rows),
		dirty:     make(chan struct{}, 1),
		agentName: o.AgentName,
		relayOn:   o.RelayEnabled,
		started:   time.Now(),
		width:     cols,
		height:    rows + statusHeight,
	}

	m.viewer = o.Terminal.NewViewer()

	// 에뮬레이터가 만든 응답을 PTY로 되돌린다.
	//
	// 자식이 "커서 위치 알려줘" 같은 질의를 보내면 에뮬레이터가 답을
	// 만들어 내부 파이프에 쓴다. 그 파이프는 버퍼가 없어서, 아무도
	// 읽지 않으면 쓰기가 막히고 에뮬레이터 뮤텍스를 쥔 채 전체가
	// 데드락에 빠진다. 반드시 읽어내야 하고, 읽은 것은 자식에게
	// 돌려줘야 질의를 기다리는 프로그램이 멈추지 않는다.
	go m.pumpReplies()

	return m
}

func (m *Model) pumpReplies() {
	buf := make([]byte, 4096)
	for {
		// 읽어서 버린다. 파이프를 비우는 것이 목적이다.
		//
		// 여기서 나오는 것을 셸에 써넣으면 안 된다. Write는 PTY의
		// stdin이라 사람이 키를 친 것과 구별되지 않고, 셸이
		// "62;1;6;22c" 같은 글자를 그대로 입력으로 받아 찍는다.
		//
		// 질의에 답하는 일은 바깥의 진짜 터미널이 이미 하고 있다.
		// 우리 에뮬레이터는 화면을 그리려고 같은 바이트를 한 번 더
		// 해석할 뿐이라, 그 답은 아무 데도 갈 필요가 없다.
		if _, err := m.emu.Read(buf); err != nil {
			return
		}
	}
}

// SetStatus는 relay 연결 상태를 갱신한다. RelayLink가 부른다.
func (m *Model) SetStatus(s relaylink.Status) {
	m.status = s
	m.markDirty()
}

func (m *Model) markDirty() {
	select {
	case m.dirty <- struct{}{}:
	default:
	}
}

func (m *Model) Init() tea.Cmd {
	// 로컬 화면도 구독자로 붙는다. replay=true라 이미 지나간 출력도 받는다.
	m.out, m.unsub = m.term.Subscribe(true)

	return tea.Batch(m.readOutput(), m.waitDirty(), m.tick())
}

// readOutput은 셸 출력을 에뮬레이터에 넣는다.
func (m *Model) readOutput() tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-m.out
		if !ok {
			return exitedMsg{}
		}
		_, _ = m.emu.Write(chunk)
		return dirtyMsg{}
	}
}

func (m *Model) waitDirty() tea.Cmd {
	return func() tea.Msg {
		<-m.dirty
		return dirtyMsg{}
	}
}

// tick은 상태바의 가동 시간을 1초마다 갱신한다.
func (m *Model) tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		cols, rows := m.shellSize()

		// PTY와 에뮬레이터를 함께 바꾼다. 하나만 하면 화면이 어긋난다.
		//
		// 셸 크기는 뷰어별로 알리고 서버가 가장 작은 쪽에 맞춘다.
		// 웹이 더 큰 창으로 보고 있어도 이 화면이 깨지지 않는다.
		_ = m.term.ResizeViewer(m.viewer, cols, rows)
		m.emu.Resize(cols, rows)
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == quitKey {
			m.quitting = true
			return m, tea.Quit
		}
		if !m.exited {
			if data := keyToBytes(msg); len(data) > 0 && !terminal.IsTerminalReply(data) {
				_ = m.term.Write(data)
			}
		}
		return m, nil

	case tea.PasteMsg:
		// 붙여넣기는 통째로 넣는다. 한 글자씩 보내면 느리고,
		// 자동 들여쓰기가 있는 편집기에서 계단 모양이 된다.
		//
		// 터미널 응답이 붙여넣기로 둔갑해 오는 경우가 있어 걸러낸다.
		if !m.exited && !terminal.IsTerminalReply([]byte(msg.Content)) {
			_ = m.term.Write([]byte(msg.Content))
		}
		return m, nil

	case dirtyMsg:
		// 출력 읽기와 dirty 대기를 둘 다 다시 건다.
		return m, tea.Batch(m.readOutput(), m.waitDirty())

	case exitedMsg:
		m.exited = true
		m.quitting = true
		return m, tea.Quit

	// kill 등으로 밖에서 끄는 경우. 시그널은 main이 ctx로 받아
	// Bubble Tea를 끝내지만, 메시지가 먼저 닿으면 여기서 받는다.
	case tea.QuitMsg:
		m.quitting = true
		return m, tea.Quit

	case statusMsg:
		m.status = relaylink.Status(msg)
		return m, nil

	case tickMsg:
		return m, m.tick()
	}

	return m, nil
}

// shellSize는 셸에게 알려줄 크기다. 상태바 높이를 뺀다.
func (m *Model) shellSize() (int, int) {
	cols, rows := m.width, m.height-statusHeight
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

func (m *Model) View() tea.View {
	var sb strings.Builder

	sb.WriteString(m.statusBar())
	sb.WriteString("\n")
	// 에뮬레이터가 해석해둔 화면을 그대로 받는다.
	// 자식의 이스케이프는 여기 오기 전에 전부 소화됐다.
	sb.WriteString(m.emu.Render())

	v := tea.NewView(sb.String())
	// v2는 옵션이 아니라 매 렌더마다 View가 선언한다.
	v.AltScreen = true

	if !m.quitting {
		// 커서를 셸 위치에 맞춘다. 상태바만큼 아래로 민다.
		pos := m.emu.CursorPosition()
		v.Cursor = tea.NewCursor(pos.X, pos.Y+statusHeight)
	}
	return v
}

func (m *Model) statusBar() string {
	cols, rows := m.shellSize()

	left := " " + m.relayPart() +
		fmt.Sprintf(" · %dx%d · %s", cols, rows, uptime(m.started))

	if n := m.term.Subscribers(); n > 1 {
		// 자기 자신도 구독자라 하나를 뺀다.
		left += fmt.Sprintf(" · 웹 %d명", n-1)
	}
	if m.exited {
		left += " · 셸 종료됨"
	}

	right := "나가기 " + quitKey + " "

	pad := m.width - runeLen(left) - runeLen(right)
	if pad < 1 {
		pad = 1
		// 너무 좁으면 오른쪽을 버린다.
		if m.width < runeLen(left) {
			return invert(truncate(left, m.width))
		}
	}
	return invert(left + strings.Repeat(" ", pad) + right)
}

func (m *Model) relayPart() string {
	if !m.relayOn {
		return "로컬 전용"
	}
	if m.status.Connected {
		name := m.agentName
		if name == "" {
			name = "이름 없음"
		}
		return "● relay 연결됨 · " + name
	}
	if m.status.LastError != "" {
		return "○ relay 끊김"
	}
	return "○ relay 연결 중"
}

// Close는 구독을 정리한다. 프로그램이 끝날 때 부른다.
func (m *Model) Close() {
	if m.unsub != nil {
		m.unsub()
	}
	// 이 화면이 나가면 남은 뷰어(웹)에 맞춰 크기가 다시 잡힌다.
	m.term.DropViewer(m.viewer)
}

// --- 표시 헬퍼 ---

func invert(s string) string { return "\x1b[7m" + s + "\x1b[0m" }

func runeLen(s string) int { return len([]rune(s)) }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(r[:n])
}

func uptime(since time.Time) string {
	d := time.Since(since)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d초", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d분", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d시간 %d분", int(d.Hours()), int(d.Minutes())%60)
	}
}
