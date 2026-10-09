#!/bin/sh
# terminal-agent를 맥에 설치하고 로그인할 때마다 켜지게 한다. (notify-agent·claudeAgent의 scripts/install.sh와 같은 규칙)
#
#   ./scripts/install.sh              설치(또는 새 판으로 바꾸기)
#   ./scripts/install.sh --uninstall  지우기
#
# 실행 파일은 저장소에서 make build 한 ./terminal-agent를 넣는다. 없으면 install.sh(curl … | sh)로
# 이미 깔아 둔 ~/.local/bin/terminal-agent를 그대로 쓴다.
#
# terminal-agent는 셸을 여는 서비스다. 늘 켜 두면 relay를 거쳐 밖에서 이 맥의 셸에 닿는다 —
# 쓸 때만 켜도 되면 등록하지 말고 필요할 때 실행한다.
#
# 설치 뒤 한 번은 사람이 해야 한다:
#   ~/.config/terminal-agent/.env 에 RELAY_TERMINAL_TOKEN(relay-service .env와 같은 값)과
#   TERMINAL_ALLOWED_ROOTS를 적고 다시 켜기: launchctl kickstart -k gui/$(id -u)/com.foncsoft.terminal-agent
set -eu

LABEL=com.foncsoft.terminal-agent
BIN_DIR="$HOME/.local/bin"
BIN="$BIN_DIR/terminal-agent"
# .env는 실행한 폴더에서 읽는다. 이 폴더에서 띄운다.
CONF_DIR="$HOME/.config/terminal-agent"
ENV_FILE="$CONF_DIR/.env"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/Library/Logs/terminal-agent.log"

if [ "${1:-}" = "--uninstall" ]; then
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$PLIST" "$BIN"
  echo "지웠습니다. 설정($CONF_DIR)과 로그($LOG)는 남겨 둡니다."
  exit 0
fi

cd "$(dirname "$0")/.."
mkdir -p "$BIN_DIR" "$CONF_DIR" "$(dirname "$PLIST")"
if [ -x ./terminal-agent ]; then
  cp ./terminal-agent "$BIN"
elif [ ! -x "$BIN" ]; then
  echo "먼저 make build 하거나 install.sh(curl … | sh)로 설치하세요." >&2
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  cp .env.example "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  echo "설정 파일을 만들었습니다: $ENV_FILE"
  echo "RELAY_TERMINAL_TOKEN·TERMINAL_ALLOWED_ROOTS를 적고 다시 켜세요."
fi

# launchd의 PATH에는 셸 설정(homebrew 등)이 없다. 터미널 안에서 쓰는 도구를 찾을 수 있게 지금 PATH를 넣는다.
PATH_VALUE="$BIN_DIR:$PATH"

cat > "$PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN</string></array>
  <key>WorkingDirectory</key><string>$CONF_DIR</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$PATH_VALUE</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$LOG</string>
  <key>StandardErrorPath</key><string>$LOG</string>
</dict>
</plist>
PLIST

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "켰습니다. 로그: $LOG"
