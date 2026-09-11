#!/bin/zsh
# Install the todo-order server as a login agent, and put the `todo` command on
# PATH.
#
# The script places files and prints the launchctl commands to run. It does not
# load the agent itself: registering a login item is watched on this machine,
# so that step is left to a person who can say why it is happening.
#
# Usage: scripts/install-agent.sh [--port N] [--ticket-url-base URL]
#
# --ticket-url-base makes a ticket key in a task name a link (for example
# "https://example.backlog.com/view/"). It lives in the generated plist rather
# than in this repository, so the tracker's address is not committed.
set -eu

PORT=8085
TICKET_URL_BASE=${TICKET_URL_BASE:-}
while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="${2:-}"; shift 2 ;;
    --port=*) PORT="${1#*=}"; shift ;;
    --ticket-url-base) TICKET_URL_BASE="${2:-}"; shift 2 ;;
    --ticket-url-base=*) TICKET_URL_BASE="${1#*=}"; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

# An existing plist already carries the base URL; keep it unless a new one was
# given, so re-running the installer does not quietly drop the links.
PLIST_EXISTING="$HOME/Library/LaunchAgents/com.user.todo-order.server.plist"
if [ -z "$TICKET_URL_BASE" ] && [ -r "$PLIST_EXISTING" ]; then
  TICKET_URL_BASE=$(plutil -extract EnvironmentVariables.TICKET_URL_BASE raw -o - "$PLIST_EXISTING" 2>/dev/null || true)
fi

case "$PORT" in
  ''|*[!0-9]*) echo "--port must be a number: '$PORT'" >&2; exit 2 ;;
esac

REPO_ROOT=${0:A:h:h}
LABEL=com.user.todo-order.server
TEMPLATE="$REPO_ROOT/launchd/$LABEL.plist.template"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
BIN_DIR="$HOME/.local/bin"
LOG_DIR="$HOME/Library/Logs/todo-order"
DATA_DIR="$HOME/.local/share/todo-order"

[ -r "$TEMPLATE" ] || { echo "template missing: $TEMPLATE" >&2; exit 1; }

# Build first: a half-installed agent that points at nothing is worse than no
# agent at all.
( cd "$REPO_ROOT" && make build )

[ -x "$REPO_ROOT/bin/server" ] || { echo "build produced no bin/server" >&2; exit 1; }
[ -x "$REPO_ROOT/bin/todo" ] || { echo "build produced no bin/todo" >&2; exit 1; }

mkdir -p "$BIN_DIR" "$LOG_DIR" "$DATA_DIR" "$HOME/Library/LaunchAgents"
chmod 700 "$DATA_DIR"

# Replace rather than copy over a running binary: rename is atomic, and `cp`
# onto a running file fails with "Text file busy".
install_binary() {
  local src=$1 dest=$2 tmp
  tmp=$(mktemp "$BIN_DIR/.todo-order-XXXXXX")
  cat "$src" > "$tmp"
  chmod 755 "$tmp"
  mv -f "$tmp" "$dest"
}

install_binary "$REPO_ROOT/bin/server" "$BIN_DIR/todo-order-server"
install_binary "$REPO_ROOT/bin/todo" "$BIN_DIR/todo"

sed -e "s|__HOME__|$HOME|g" -e "s|__PORT__|$PORT|g" \
    -e "s|__TICKET_URL_BASE__|$TICKET_URL_BASE|g" "$TEMPLATE" > "$PLIST.tmp"
mv -f "$PLIST.tmp" "$PLIST"
chmod 644 "$PLIST"

plutil -lint "$PLIST" >/dev/null || { echo "generated plist is not valid: $PLIST" >&2; exit 1; }

cat <<EOF

placed:
  $BIN_DIR/todo-order-server
  $BIN_DIR/todo
  $PLIST        (port $PORT, loopback only)
  ticket links: ${TICKET_URL_BASE:-（未設定。--ticket-url-base で指定する）}
  logs:  $LOG_DIR
  data:  $DATA_DIR

The agent is not loaded yet. Registering a login item is monitored on this
machine, so run these yourself when you are ready:

  launchctl bootstrap gui/\$(id -u) "$PLIST"
  launchctl kickstart -k "gui/\$(id -u)/$LABEL"

Then check it:

  curl -s http://127.0.0.1:$PORT/healthz
  todo list

To remove it:

  launchctl bootout "gui/\$(id -u)/$LABEL"
  rm "$PLIST"
EOF
