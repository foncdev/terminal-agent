# terminal-agent

**터미널을 어디서든 이어 쓰는 에이전트.** 데스크톱에서 쓰던 셸을 브라우저나
스마트 안경에서 그대로 이어서 본다.

실행하면 그 자리에 콘솔 화면이 뜬다. 같은 셸이 다른 화면에도 보이고, 한쪽에서
친 명령이 다른 쪽에도 나타난다.

```
 ● relay 연결됨 · my-mac · 100x29 · 3분 · 웹 1명        나가기 ctrl+\
 $ echo hello          ← 데스크톱에서 침
 hello
 $ ls -la              ← 브라우저에서 보냄
 ...
```

`vim`, `top`, 색상, 탭 완성이 전부 동작한다. 파이프가 아니라 의사 터미널(PTY)
위에서 돌기 때문이다.

맥·리눅스·윈도우에서 돌고, **런타임 의존성이 없는 단일 바이너리**다.

> **경고**
> 셸을 여는 서비스다. 실행하면 그 계정으로 할 수 있는 모든 일을 할 수 있다.
> 인터넷에 직접 열지 말 것. 자세한 것은 [보안](#보안) 참고.

---

## 설치

### 한 줄 설치 (추천)

```bash
curl -fsSL https://raw.githubusercontent.com/foncdev/terminal-agent/main/install.sh | sh
```

이 방식으로 받으면 **macOS Gatekeeper 경고가 없다.** 브라우저로 직접 받으면
격리 속성이 붙어 "확인되지 않은 개발자" 경고가 뜨지만, 스크립트로 받으면
붙지 않는다.

설치 위치는 `/usr/local/bin`이고, 쓰기 권한이 없으면 `~/.local/bin`에 넣는다.

```bash
# 위치나 버전을 지정하려면
BIN_DIR=~/bin VERSION=v0.1.0 sh -c "$(curl -fsSL https://raw.githubusercontent.com/foncdev/terminal-agent/main/install.sh)"
```

윈도우는 [Release 페이지](https://github.com/foncdev/terminal-agent/releases)에서
직접 받는다.

### 소스에서 빌드

```bash
git clone https://github.com/foncdev/terminal-agent.git
cd terminal-agent
make build          # ./terminal-agent 생성
```

### go install

```bash
go install github.com/foncdev/terminal-agent/cmd/terminal-agent@latest
```

### 요구 사항

- Go 1.25 이상 (빌드할 때만)
- 윈도우는 **Windows 10 1809 이상** — ConPTY가 필요하다

CGO를 쓰지 않으므로 C 컴파일러나 별도 툴체인이 필요 없다.

---

## 빠른 시작

```bash
cp .env.example .env
```

`.env`를 열어 두 줄만 채운다:

```bash
TERMINAL_ENABLED=true              # 이게 없으면 시작하지 않는다
TERMINAL_ALLOWED_ROOTS=~/projects  # 터미널을 열 수 있는 디렉터리
```

```bash
make run
```

콘솔 화면이 뜬다. **나갈 때는 `ctrl+\`** — `ctrl+c`는 셸로 전달된다.

서버로만 돌리려면:

```bash
./terminal-agent --headless
```

터미널에 붙어 있지 않으면(systemd, nohup 등) 알아서 서버 모드로 돈다.

---

## 빌드

```bash
make build     # 현재 플랫폼
make test      # vet + 테스트
make dist      # 5개 플랫폼 바이너리를 dist/ 에
make clean
```

`make dist`는 빌드 머신 하나에서 전부 만들어 `tar.gz`로 묶는다:

```
dist/
  terminal-agent_darwin_arm64.tar.gz
  terminal-agent_darwin_amd64.tar.gz
  terminal-agent_linux_amd64.tar.gz
  terminal-agent_linux_arm64.tar.gz
  terminal-agent_windows_amd64.tar.gz
  checksums.txt
```

각 3MB 남짓이고 풀면 바로 돈다. 파일 이름은 `install.sh`가 그대로 쓰므로
바꾸려면 양쪽을 같이 고쳐야 한다.

### 릴리스

```bash
git tag v0.1.0
make dist                 # 태그가 버전으로 박힌다
gh release create v0.1.0 dist/*.tar.gz dist/checksums.txt
```

버전은 `git describe`에서 가져오고 `-X main.version`으로 주입된다.
`terminal-agent --version`으로 확인할 수 있다.

---

## 사용법

### 혼자 쓰기

```bash
make run
```

터미널 창에 콘솔이 뜬다. 평소 셸처럼 쓰면 된다.

| 키 | 동작 |
|---|---|
| `ctrl+\` | 프로그램 종료 |
| `ctrl+c` | 셸로 전달 (실행 중인 명령 중단) |
| `exit` | 셸 종료 → 프로그램도 함께 종료 |

### 다른 화면에서 같이 보기

에이전트를 띄운 뒤 `:4200`에 붙는 클라이언트를 만든다. 브라우저든 스마트
안경이든 **같은 HTTP API를 쓴다** — 출력을 받아 그리고 입력을 보내면 된다.

브라우저라면 [xterm.js](https://xtermjs.org/)로 20줄이면 된다:

```js
const term = new Terminal();
term.open(document.getElementById('app'));

// 터미널 만들기
const { terminal } = await fetch('/terminals', {
  method: 'POST',
  headers: { 'content-type': 'application/json', 'x-api-key': KEY },
  body: JSON.stringify({ cols: term.cols, rows: term.rows }),
}).then((r) => r.json());

// 출력 받기 (base64로 온다)
const es = new EventSource(`/terminals/${terminal.id}/stream?token=${KEY}`);
es.addEventListener('output', (e) => term.write(base64ToBytes(e.data)));

// 입력 보내기
term.onData((data) =>
  fetch(`/terminals/${terminal.id}/input`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-api-key': KEY },
    body: JSON.stringify({ data }),
  }),
);
```

`WebSocket`(`/terminals/{id}/ws`)을 쓰면 입력·출력을 한 연결로 처리할 수 있다.

화면이 좁은 기기(스마트 안경 등)는 터미널을 그대로 그리기 어렵다. 그럴 때는
출력의 마지막 몇 줄만 보여주거나, 미리 정한 명령을 골라 보내는 식으로 쓴다.
API는 같으므로 클라이언트가 어떻게 그릴지만 정하면 된다.

### 밖에서 접속하기

공유기 안에 있는 기기에 밖에서 붙으려면 중계 서버를 둔다. 에이전트가 **자기
쪽에서 나가서** 붙으므로 포트포워딩이 필요 없다.

```bash
RELAY_URL=ws://<중계서버>:4100/terminal-agent
RELAY_TERMINAL_TOKEN=<중계 서버와 맞춘 값>
```

중계 서버 쪽 프로토콜은 [중계 서버 연동](#중계-서버-연동)에 있다.

---

## API

`TERMINAL_API_KEY`를 설정하면 `x-api-key` 헤더가 필요하다. `/health`는 예외.

브라우저의 `WebSocket`과 `EventSource`는 헤더를 못 붙이므로, 스트림 경로에
한해 `?token=`도 받는다.

```
GET    /health                     인증 없이 열림
POST   /terminals                  { dir?, argv?, cols, rows } → 201
GET    /terminals                  목록
GET    /terminals/{id}             단건
DELETE /terminals/{id}             종료 → 204
POST   /terminals/{id}/input       { data }
POST   /terminals/{id}/resize      { cols, rows } → { ok, cols, rows }
GET    /terminals/{id}/ws          WebSocket (양방향)
GET    /terminals/{id}/stream      SSE (출력만)
```

### 예시

```bash
KEY=$(grep TERMINAL_API_KEY .env | cut -d= -f2)

ID=$(curl -s -X POST localhost:4200/terminals \
  -H "x-api-key: $KEY" -H 'content-type: application/json' \
  -d '{"cols":100,"rows":30}' | jq -r .terminal.id)

curl -sN "localhost:4200/terminals/$ID/stream?token=$KEY" &

curl -s -X POST "localhost:4200/terminals/$ID/input" \
  -H "x-api-key: $KEY" -H 'content-type: application/json' \
  -d '{"data":"ls -la\n"}'
```

### WebSocket 프로토콜

키 입력마다 오가므로 키를 짧게 뒀다.

```
클라 → 서버   {"t":"i","d":"ls\r"}       입력
             {"t":"r","c":120,"r":40}    리사이즈
서버 → 클라   {"t":"o","d":"<base64>"}   출력
             {"t":"x","code":0}          종료
```

**출력은 base64로 감싼다.** PTY는 임의 바이트를 뱉는데, UTF-8 문자 중간에서
잘린 조각이 섞이면 JSON 인코딩이 깨진다. 잘린 조각도 그대로 실어 보내야
클라이언트가 이어 붙여 복원할 수 있다.

`?replay=0`을 붙이면 이전 출력 재생을 건너뛴다. 기본은 재생이라 늦게 붙어도
하던 작업이 보인다.

### 여러 화면이 같이 볼 때

셸 크기는 **가장 작은 화면**에 맞춘다(tmux와 같은 방식). 큰 쪽에 맞추면 작은
화면에서 줄이 접혀 `vim` 같은 앱의 마지막 줄이 엉뚱한 자리에 찍힌다.

`/resize` 응답에 실제로 정해진 `cols`/`rows`가 담겨 오므로, 클라이언트는 그
값으로 자기 화면을 맞추면 된다.

---

## 설정

| 변수 | 기본값 | 설명 |
|---|---|---|
| `TERMINAL_ENABLED` | (없음) | `true`가 아니면 시작하지 않는다 |
| `TERMINAL_HOST` | `127.0.0.1` | 리스닝 주소 |
| `TERMINAL_PORT` | `4200` | 리스닝 포트 |
| `TERMINAL_API_KEY` | (없음) | 설정 시 `x-api-key` 필수 |
| `TERMINAL_ALLOWED_ROOTS` | 홈 디렉터리 | 터미널 시작 가능 경로. unix `:`, windows `;` |
| `TERMINAL_SHELL` | (없음) | 비우면 `$SHELL` / PowerShell |
| `TERMINAL_MAX` | `3` | 동시 터미널 수 |
| `TERMINAL_IDLE_MINUTES` | `30` | 이만큼 조용하면 셸 정리 |
| `TERMINAL_SCROLLBACK_BYTES` | `262144` | 재생용 최근 출력량 |
| `TERMINAL_LOG` | 임시 디렉터리 | 콘솔 화면이 떠 있을 때 로그 파일 |
| `RELAY_URL` | (없음) | 중계 서버로 나가서 붙는다 (선택) |
| `RELAY_TERMINAL_TOKEN` | (없음) | 중계 서버와 맞춘 토큰 |
| `RELAY_AGENT_NAME` | 호스트명 | 서버 목록에 보일 이름 |

`.env` 파일도 읽지만 **셸에 이미 있는 값이 우선**한다.

---

## 보안

셸은 도구 승인이나 경로 제한으로 막을 수 있는 물건이 아니다. 다른 층위로 막는다.

1. **기본 비활성** — `TERMINAL_ENABLED=true`가 있어야 뜬다
2. **시작 디렉터리 제한** — `EvalSymlinks`로 해석한 뒤 검사해 심볼릭 링크
   탈출을 막는다. **셸 안에서 `cd`로 나가는 것은 막지 않는다** — 실수를 줄이는
   장치이지 보안 경계가 아니다
3. **유휴 정리와 개수 제한** — 잊어버린 셸이 쌓이지 않게
4. **터미널 응답 필터** — 바깥 터미널이 보낸 장치 응답이 셸 입력으로 새지 않게
   막는다. 안 막으면 `1;2c` 같은 글자가 저절로 찍힌다
5. **윈도우** — `.bat`/`.cmd`와 `cmd.exe` 직접 실행을 막는다. 인용 규칙이
   달라 인자에 섞인 특수문자가 빠져나갈 수 있다

**인터넷에 직접 열지 마라.** 밖에서 써야 한다면 중계 서버를 두고 그쪽에
인증을 걸어라.

---

## 동작 방식

### 플랫폼별 PTY

유닉스와 윈도우는 생김새가 꽤 다르다. `internal/pty`가 그 차이를 흡수하고,
나머지 코드는 플랫폼을 모른다.

| 플랫폼 | 라이브러리 |
|---|---|
| unix | [creack/pty](https://github.com/creack/pty) |
| windows | [UserExistsError/conpty](https://github.com/UserExistsError/conpty) |

**`creack/pty`는 윈도우에서 컴파일만 되고 동작하지 않는다** —
`start_windows.go`가 `ErrUnsupported`를 반환하는 한 줄이다. 빌드가 통과한다고
되는 줄 알면 런타임에 터진다. 그래서 빌드 태그로 갈랐다.

두 API의 차이:

| | unix | windows |
|---|---|---|
| 명령 전달 | `*exec.Cmd` (argv 배열) | 명령줄 **문자열** |
| 이유 | — | ConPTY가 `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`를 붙여야 해서 `os/exec`를 못 쓴다 |

바깥 코드는 계속 argv 배열을 다루고, 문자열로 합치는 일은 윈도우 구현
안에서만 일어난다(`syscall.EscapeArg` 사용).

### 콘솔 화면

[Bubble Tea](https://github.com/charmbracelet/bubbletea)로 상단 한 줄을
고정하고, 그 아래에서 셸이 돈다.

**왜 에뮬레이터를 거치는가.** Bubble Tea는 화면을 셀 단위로 관리하는데, 자식이
뱉는 이스케이프가 같은 곳으로 나가면 서로 화면을 망친다. `vim`이 1행에 그리려
하면 상태바를 덮어버린다. 그래서 자식의 출력을
[charmbracelet/x/vt](https://github.com/charmbracelet/x)로 먼저 해석해 격자로
만들고, 그 격자만 Bubble Tea에 넘긴다. 에뮬레이터가 방화벽이다.

```
PTY ──▶ 에뮬레이터(격자) ──Render()──▶ Bubble Tea View
    └──▶ 웹 구독자들

키 입력 ──keyToBytes──▶ Terminal.Write ──▶ PTY
```

콘솔 화면도 웹과 **같은 자격의 구독자**다. 따로 처리하는 경로가 없다.

### 중계 서버 연동

`RELAY_URL`을 주면 에이전트가 그쪽으로 WebSocket을 건다. 서버는 그 연결로
요청을 되돌려 보낸다.

```
서버 → agent : {"type":"welcome",      "agentId"}
               {"type":"request",      "id","method","path","body"}
               {"type":"stream_open",  "id","path"}
               {"type":"stream_close", "id"}
agent → 서버 : {"type":"response",     "id","status","body"}
               {"type":"error",        "id","message"}
               {"type":"stream_chunk", "id","chunk","done"}
```

에이전트는 받은 요청을 자기 HTTP API로 대신 호출해 결과를 돌려준다.
끊기면 2초 → 60초로 간격을 늘려가며 다시 붙는다.

---

## 구조

```
cmd/terminal-agent/main.go   기동, 설정, 시그널 처리
internal/
  config/     환경변수 + 경로 제한
  pty/        플랫폼별 PTY를 하나의 인터페이스로
  terminal/   Terminal(구독자·링버퍼) + Registry + 응답 필터
  api/        HTTP 라우트 + WebSocket/SSE
  relaylink/  중계 서버로 나가는 WS (선택)
  tui/        콘솔 화면
```

### 설계 메모

- **PTY는 고루틴 하나가 독점해서 읽는다.** 여러 곳에서 읽으면 바이트가 섞인다
- **느린 구독자는 건너뛴다.** 채널 버퍼가 차면 그 구독자에게는 보내지 않는다.
  화면이 좀 튀는 편이, 한 클라이언트 때문에 전부 멈추는 것보다 낫다
- **스크롤백은 링버퍼.** 터미널 출력은 끝이 없어서 전부 쌓으면 메모리가 샌다
- **에뮬레이터 응답은 버린다.** 안 읽으면 내부 파이프가 막혀 데드락에 빠지고,
  PTY에 되돌리면 셸이 키 입력으로 받아 글자가 찍힌다

---

## 테스트

```bash
make test
```

덮는 것:

- **경로 제한** — 심볼릭 링크 탈출, 이름이 겹치는 형제 디렉터리(`/a/x` vs
  `/a/x-evil`), `..` 탈출
- **링버퍼** — 무작위 500회가 늘 "마지막 N바이트"와 같은지
- **실제 셸** — TTY 여부, 크기, 리사이즈, 한글, 종료 코드, 느린 구독자
- **키 변환** — enter/backspace/방향키/F키/ctrl 조합/한글
- **응답 필터** — 실제로 새던 바이트 16종을 막고, 키 입력 24종은 통과하는지
- **콘솔 화면** — 바이너리를 가짜 터미널에서 돌리고 그 화면을 에뮬레이터로
  읽어 검사한다. 상태바 고정, 셸 크기, 전체화면 앱이 상태바를 덮지 않는지,
  종료 경로 4가지

TUI는 사람이 봐야 아는 물건이라, 사람이 볼 화면을 프로그램이 읽게 만들었다.

PTY 테스트는 윈도우에서 건너뛴다. 실기기에서 확인해야 한다.

---

## 관련 링크

- [xterm.js](https://xtermjs.org/) — 브라우저 터미널 렌더러
- [creack/pty](https://github.com/creack/pty) — unix PTY
- [UserExistsError/conpty](https://github.com/UserExistsError/conpty) — Windows ConPTY
- [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) — TUI 프레임워크
- [charmbracelet/x/vt](https://github.com/charmbracelet/x) — 터미널 에뮬레이터
- [coder/websocket](https://github.com/coder/websocket) — WebSocket

---

## 라이선스

MIT — [LICENSE](LICENSE) 참고.
