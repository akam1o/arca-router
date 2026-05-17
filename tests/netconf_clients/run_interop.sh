#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <python-test-script>" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

EVIDENCE_DIR="${NETCONF_INTEROP_EVIDENCE_DIR:-}"
if [[ -n "$EVIDENCE_DIR" ]]; then
  mkdir -p "$EVIDENCE_DIR"
  EVIDENCE_DIR="$(cd "$EVIDENCE_DIR" && pwd)"
  export NETCONF_INTEROP_EVIDENCE_DIR="$EVIDENCE_DIR"
fi

collect_evidence() {
  if [[ -z "$EVIDENCE_DIR" ]]; then
    return
  fi
  cp -f "$TMPDIR/running.conf" "$EVIDENCE_DIR/running.conf" 2>/dev/null || true
  cp -f "$TMPDIR/netconf-interop-server.log" "$EVIDENCE_DIR/server.log" 2>/dev/null || true
  cp -f "$TMPDIR/client.stdout" "$EVIDENCE_DIR/client.stdout" 2>/dev/null || true
  cp -f "$TMPDIR/client.stderr" "$EVIDENCE_DIR/client.stderr" 2>/dev/null || true
}

cleanup() {
  if [[ -n "${DAEMON_PID:-}" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  collect_evidence
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

chmod 700 "$TMPDIR"

cat >"$TMPDIR/running.conf" <<'CONFIG'
set system host-name arca-ci
set interfaces ge-0/0/0 description "interop-uplink"
set interfaces xe-0/0/0 description "interop-peer"
CONFIG

if [[ -n "$EVIDENCE_DIR" ]]; then
  {
    echo "script=$SCRIPT"
    echo "host=$HOST"
    echo "port=$PORT"
    go version
    "${PYTHON:-python3}" --version
  } >"$EVIDENCE_DIR/metadata.txt"
fi

go run -buildvcs=false ./tools/netconf-userdb \
  -path "$TMPDIR/users.db" \
  -username "$USERNAME" \
  -password "$PASSWORD" \
  -role admin

go build -buildvcs=false -o "$TMPDIR/netconf-interop-server" ./tools/netconf-interop-server

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

client_args=(
  --host "$HOST"
  --port "$PORT"
  --username "$USERNAME"
  --password "$PASSWORD"
)
if [[ -n "$EVIDENCE_DIR" ]]; then
  client_args+=(--evidence-dir "$EVIDENCE_DIR")
fi

if ! "${PYTHON:-python3}" "$ROOT/$SCRIPT" "${client_args[@]}" \
  >"$TMPDIR/client.stdout" 2>"$TMPDIR/client.stderr"; then
  cat "$TMPDIR/client.stdout"
  cat "$TMPDIR/client.stderr" >&2
  cat "$TMPDIR/netconf-interop-server.log" >&2
  exit 1
fi
cat "$TMPDIR/client.stdout"
cat "$TMPDIR/client.stderr" >&2
