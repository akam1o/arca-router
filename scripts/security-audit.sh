#!/usr/bin/env bash
set -uo pipefail

UNIT="${ARCA_SECURITY_UNIT:-arca-routerd.service}"
DAEMON_USER="${ARCA_SECURITY_USER:-arca-router}"

failures=0
warnings=0

pass() {
  printf 'PASS: %s\n' "$*"
}

warn() {
  warnings=$((warnings + 1))
  printf 'WARN: %s\n' "$*" >&2
}

fail() {
  failures=$((failures + 1))
  printf 'FAIL: %s\n' "$*" >&2
}

have() {
  command -v "$1" >/dev/null 2>&1
}

systemd_value() {
  local prop="$1"
  if ! have systemctl; then
    return 1
  fi
  systemctl show "$UNIT" -p "$prop" --value 2>/dev/null
}

stat_field() {
  local path="$1"
  local field="$2"
  stat -c "$field" "$path" 2>/dev/null
}

check_systemd_unit() {
  if ! have systemctl; then
    warn "systemctl not available; skipping systemd checks"
    return
  fi

  local user
  user="$(systemd_value User || true)"
  if [[ -z "$user" ]]; then
    fail "$UNIT has no User= setting"
  elif [[ "$user" == "root" ]]; then
    fail "$UNIT runs as root"
  elif [[ "$user" == "$DAEMON_USER" ]]; then
    pass "$UNIT runs as $DAEMON_USER"
  else
    warn "$UNIT runs as unexpected user: $user"
  fi

  local groups
  groups="$(systemd_value SupplementaryGroups || true)"
  for group in vpp frrvty; do
    if [[ " $groups " == *" $group "* ]]; then
      pass "$UNIT has supplementary group $group"
    else
      fail "$UNIT missing supplementary group $group"
    fi
  done

  local caps
  caps="$(systemd_value AmbientCapabilities || true)"
  for cap in CAP_NET_ADMIN CAP_NET_BIND_SERVICE; do
    if [[ "$caps" == *"$cap"* ]]; then
      pass "$UNIT has $cap"
    else
      fail "$UNIT missing $cap"
    fi
  done
  if [[ "$caps" == *"CAP_SYS_ADMIN"* ]]; then
    fail "$UNIT grants CAP_SYS_ADMIN"
  fi

  local rw_paths
  rw_paths="$(systemd_value ReadWritePaths || true)"
  if [[ "$rw_paths" == *"/etc/frr"* ]]; then
    fail "$UNIT grants default /etc/frr writes; use a local drop-in only for file backend"
  else
    pass "$UNIT does not grant default /etc/frr writes"
  fi
}

check_daemon_process() {
  if ! have pgrep || ! have ps; then
    warn "pgrep or ps not available; skipping running process user check"
    return
  fi

  local pids
  pids="$(pgrep -x arca-routerd 2>/dev/null || true)"
  if [[ -z "$pids" ]]; then
    warn "arca-routerd is not running; skipping process owner check"
    return
  fi

  local pid owner
  for pid in $pids; do
    owner="$(ps -o user= -p "$pid" 2>/dev/null | awk '{print $1}')"
    if [[ "$owner" == "root" ]]; then
      fail "arca-routerd pid $pid runs as root"
    else
      pass "arca-routerd pid $pid runs as $owner"
    fi
  done
}

check_regular_file() {
  local path="$1"
  local expected_owner="$2"
  local expected_group="$3"
  local expected_mode="$4"

  if [[ ! -e "$path" ]]; then
    warn "$path not found"
    return
  fi
  if [[ ! -f "$path" ]]; then
    fail "$path is not a regular file"
    return
  fi

  local owner group mode
  owner="$(stat_field "$path" '%U')"
  group="$(stat_field "$path" '%G')"
  mode="$(stat_field "$path" '%a')"

  [[ "$owner" == "$expected_owner" ]] || fail "$path owner is $owner, want $expected_owner"
  [[ "$group" == "$expected_group" ]] || fail "$path group is $group, want $expected_group"
  [[ "$mode" == "$expected_mode" ]] || fail "$path mode is $mode, want $expected_mode"

  if [[ "$owner" == "$expected_owner" && "$group" == "$expected_group" && "$mode" == "$expected_mode" ]]; then
    pass "$path owner/group/mode are $owner:$group $mode"
  fi
}

check_vpp_socket() {
  local path="${ARCA_VPP_SOCKET:-/run/vpp/api.sock}"
  if [[ ! -e "$path" ]]; then
    warn "$path not found; VPP may not be running"
    return
  fi
  if [[ ! -S "$path" ]]; then
    fail "$path is not a socket"
    return
  fi

  local group mode
  group="$(stat_field "$path" '%G')"
  mode="$(stat_field "$path" '%a')"
  [[ "$group" == "vpp" ]] || fail "$path group is $group, want vpp"
  if (( (8#$mode & 0020) == 0 )); then
    fail "$path is not group-writable: mode $mode"
  else
    pass "$path is group-writable for group $group"
  fi
}

check_frr_runtime() {
  if have id; then
    if id "$DAEMON_USER" >/dev/null 2>&1; then
      local groups
      groups="$(id -nG "$DAEMON_USER" 2>/dev/null || true)"
      for group in vpp frrvty; do
        if [[ " $groups " == *" $group "* ]]; then
          pass "$DAEMON_USER is in group $group"
        else
          fail "$DAEMON_USER is missing group $group"
        fi
      done
    else
      fail "user $DAEMON_USER does not exist"
    fi
  fi

  if [[ -e /etc/frr/frr.conf ]]; then
    local group mode
    group="$(stat_field /etc/frr/frr.conf '%G')"
    mode="$(stat_field /etc/frr/frr.conf '%a')"
    if [[ "$group" != "frr" ]]; then
      warn "/etc/frr/frr.conf group is $group, want frr for file backend"
    fi
    if (( (8#$mode & 0020) == 0 )); then
      warn "/etc/frr/frr.conf is not group-writable; file backend will be unavailable"
    else
      pass "/etc/frr/frr.conf is group-writable for file backend"
    fi
  else
    warn "/etc/frr/frr.conf not found; skipping file backend permission check"
  fi
}

main() {
  printf '=== arca-router Security Audit ===\n'
  printf 'unit=%s user=%s\n' "$UNIT" "$DAEMON_USER"

  check_systemd_unit
  check_daemon_process
  check_regular_file /etc/arca-router/arca-router.conf root arca-router 640
  check_regular_file /etc/arca-router/hardware.yaml root arca-router 640
  check_vpp_socket
  check_frr_runtime

  printf 'summary: failures=%d warnings=%d\n' "$failures" "$warnings"
  if (( failures > 0 )); then
    exit 1
  fi
}

main "$@"
