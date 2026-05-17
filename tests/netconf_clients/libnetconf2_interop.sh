#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMPDIR="$(mktemp -d)"
USERNAME="libnetconf2-ci"
PASSWORD="NetconfInterop123"
HOST="127.0.0.1"
PORT="$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"

cd "$ROOT"

EVIDENCE_DIR="${NETCONF_INTEROP_EVIDENCE_DIR:-}"
if [[ -n "$EVIDENCE_DIR" ]]; then
  mkdir -p "$EVIDENCE_DIR/rpc" "$EVIDENCE_DIR/reply"
  EVIDENCE_DIR="$(cd "$EVIDENCE_DIR" && pwd)"
  export NETCONF_INTEROP_EVIDENCE_DIR="$EVIDENCE_DIR"
fi

collect_evidence() {
  if [[ -z "$EVIDENCE_DIR" ]]; then
    return
  fi
  cp -f "$TMPDIR/running.conf" "$EVIDENCE_DIR/running.conf" 2>/dev/null || true
  cp -f "$TMPDIR/netconf-interop-server.log" "$EVIDENCE_DIR/server.log" 2>/dev/null || true
}

cleanup() {
  if [[ -n "${DAEMON_PID:-}" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  collect_evidence
  if [[ "${KEEP_LIBNETCONF2_INTEROP_TMP:-0}" != "1" ]]; then
    rm -rf "$TMPDIR"
  else
    echo "kept temporary libnetconf2 interop directory: $TMPDIR" >&2
  fi
}
trap cleanup EXIT

chmod 700 "$TMPDIR"

CLIENT_KEY="$TMPDIR/libnetconf2_client_ed25519"
ssh-keygen -t ed25519 -N '' -C "$USERNAME" -f "$CLIENT_KEY" >/dev/null

cat >"$TMPDIR/running.conf" <<'CONFIG'
set system host-name arca-ci
set interfaces ge-0/0/0 description "interop-uplink"
set interfaces xe-0/0/0 description "interop-peer"
CONFIG

if [[ -n "$EVIDENCE_DIR" ]]; then
  {
    echo "client=libnetconf2"
    echo "standard_xpath=${NETCONF_STANDARD_XPATH:-0}"
    echo "host=$HOST"
    echo "port=$PORT"
    go version
    pkg-config --modversion libnetconf2 | sed 's/^/libnetconf2=/'
    pkg-config --modversion libyang | sed 's/^/libyang=/'
    cc --version | head -n 1
  } >"$EVIDENCE_DIR/metadata.txt"
fi

go run -buildvcs=false ./tools/netconf-userdb \
  -path "$TMPDIR/users.db" \
  -username "$USERNAME" \
  -password "$PASSWORD" \
  -role admin \
  -public-key-file "$CLIENT_KEY.pub" \
  -public-key-comment "$USERNAME"

go build -buildvcs=false -o "$TMPDIR/netconf-interop-server" ./tools/netconf-interop-server

SCHEMA_DIR="$TMPDIR/libnetconf2-schemas"
mkdir -p "$SCHEMA_DIR"
if [[ -d /usr/share/yang/modules/libyang ]]; then
  find /usr/share/yang/modules/libyang -maxdepth 1 -name '*.yang' -exec cp -f {} "$SCHEMA_DIR" \;
fi

ACM_SCHEMA_SOURCE="$SCHEMA_DIR/ietf-netconf-acm.yang"
if [[ ! -r "$ACM_SCHEMA_SOURCE" && -r /usr/share/doc/libyang2-tools/examples/ietf-netconf-acm.yang ]]; then
  cp /usr/share/doc/libyang2-tools/examples/ietf-netconf-acm.yang "$ACM_SCHEMA_SOURCE"
fi
if [[ ! -r "$ACM_SCHEMA_SOURCE" ]] && command -v apt-get >/dev/null 2>&1 && command -v dpkg-deb >/dev/null 2>&1; then
  mkdir -p "$TMPDIR/libyang2-tools-deb"
  (
    cd "$TMPDIR/libyang2-tools-deb"
    apt-get -o APT::Sandbox::User=root download libyang2-tools >/dev/null 2>/dev/null &&
      dpkg-deb -x libyang2-tools_*.deb extracted &&
      cp extracted/usr/share/doc/libyang2-tools/examples/ietf-netconf-acm.yang "$ACM_SCHEMA_SOURCE"
  ) || true
fi
if [[ ! -r "$ACM_SCHEMA_SOURCE" ]]; then
  cat >"$ACM_SCHEMA_SOURCE" <<'YANG'
module ietf-netconf-acm {
  yang-version 1.1;
  namespace "urn:ietf:params:xml:ns:yang:ietf-netconf-acm";
  prefix nacm;

  description
    "Minimal NACM module stub used by the libnetconf2 interoperability
     smoke test when the distribution package does not ship example YANG
     modules. The test only needs the module to exist in the client context.";

  extension default-deny-write {
    description
      "Placeholder for the NACM default-deny-write extension used by
       ietf-netconf.";
  }

  extension default-deny-all {
    description
      "Placeholder for the NACM default-deny-all extension used by
       ietf-netconf.";
  }
}
YANG
fi

cat >"$SCHEMA_DIR/ietf-interfaces.yang" <<'YANG'
module ietf-interfaces {
  namespace "urn:ietf:params:xml:ns:yang:ietf-interfaces";
  prefix if;

  container interfaces {
    list interface {
      key "name";
      leaf name {
        type string;
      }
      leaf description {
        type string;
      }
    }
  }
}
YANG

export LIBNETCONF2_SCHEMA_SEARCHPATH="${LIBNETCONF2_SCHEMA_SEARCHPATH:-$SCHEMA_DIR}"

server_args=(
  --datastore "$TMPDIR/config.db"
  --host-key "$TMPDIR/ssh_host_ed25519_key"
  --user-db "$TMPDIR/users.db"
  --listen "$HOST:$PORT"
  --running-config "$TMPDIR/running.conf"
)
if [[ "${NETCONF_STANDARD_XPATH:-0}" == "1" ]]; then
  server_args+=(--standard-xpath)
fi

"$TMPDIR/netconf-interop-server" "${server_args[@]}" \
  >"$TMPDIR/netconf-interop-server.log" 2>&1 &
DAEMON_PID=$!

READY=0
for _ in $(seq 1 100); do
  if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
    cat "$TMPDIR/netconf-interop-server.log" >&2
    exit 1
  fi
  if python3 - <<PY
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

cc -Wall -Wextra -Werror \
  tests/netconf_clients/libnetconf2_xpath_interop.c \
  -o "$TMPDIR/libnetconf2-xpath-interop" \
  $(pkg-config --cflags --libs libnetconf2) \
  $(pkg-config --cflags --libs libyang) \
  $(pkg-config --cflags --libs libssh)

if ! "$TMPDIR/libnetconf2-xpath-interop" "$HOST" "$PORT" "$USERNAME" "$CLIENT_KEY.pub" "$CLIENT_KEY"; then
  cat "$TMPDIR/netconf-interop-server.log" >&2
  exit 1
fi
