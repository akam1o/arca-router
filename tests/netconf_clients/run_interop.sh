#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <python-test-script>" >&2
  exit 2
fi

ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="$1"
TMPDIR="$(mktemp -d)"
PASSWORD="NetconfInterop123"
USERNAME="admin"
HOST="127.0.0.1"
PORT="$("${PYTHON:-python3}" - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"

cd "$ROOT"

cleanup() {
  if [[ -n "${DAEMON_PID:-}" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

chmod 700 "$TMPDIR"

cat >"$TMPDIR/running.conf" <<'CONFIG'
set system host-name arca-ci
set interfaces ge-0/0/0 description "interop-uplink"
set interfaces xe-0/0/0 description "interop-peer"
CONFIG

go run ./tools/netconf-userdb \
  -path "$TMPDIR/users.db" \
  -username "$USERNAME" \
  -password "$PASSWORD" \
  -role admin

go build -o "$TMPDIR/netconf-interop-server" ./tools/netconf-interop-server

"$TMPDIR/netconf-interop-server" \
  --datastore "$TMPDIR/config.db" \
  --host-key "$TMPDIR/ssh_host_ed25519_key" \
  --user-db "$TMPDIR/users.db" \
  --listen "$HOST:$PORT" \
  --running-config "$TMPDIR/running.conf" \
  >"$TMPDIR/netconf-interop-server.log" 2>&1 &
DAEMON_PID=$!

READY=0
for _ in $(seq 1 100); do
  if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
    cat "$TMPDIR/netconf-interop-server.log" >&2
    exit 1
  fi
  if "${PYTHON:-python3}" - <<PY
import socket
import sys

try:
    with socket.create_connection(("$HOST", $PORT), timeout=0.2):
        pass
except OSError:
    sys.exit(1)
PY
  then
    READY=1
    break
  fi
  sleep 0.1
done

if [[ "$READY" -ne 1 ]]; then
  cat "$TMPDIR/netconf-interop-server.log" >&2
  exit 1
fi

if ! "${PYTHON:-python3}" "$ROOT/$SCRIPT" \
  --host "$HOST" \
  --port "$PORT" \
  --username "$USERNAME" \
  --password "$PASSWORD"; then
  cat "$TMPDIR/netconf-interop-server.log" >&2
  exit 1
fi
