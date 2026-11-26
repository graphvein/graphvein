#!/usr/bin/env bash
# Throwaway PostgreSQL 17 cluster for Claude Code cloud sessions.
#
# Cloud sessions only (Linux cloud containers set up by scripts/cloud-setup.sh).
# Never run this on the developer's machine: there Postgres runs only in CI.
# This is the AGENTS.md exception: initdb into a scratch directory outside the
# repository, listen on a Unix socket in that directory and on 127.0.0.1 only,
# run as an unprivileged user through pg_ctl, no system service, no Docker.
#
# Usage:
#   bash scripts/cloud-postgres.sh [--allow-16] start|stop|url|status [DIR]
#
# DIR defaults to $GV_PG_DIR, then /tmp/graphvein-pg. It holds the whole
# cluster (data/, the socket, the log and the port) and nothing else; stop
# deletes it. When the script runs as root the server runs as the "postgres"
# user (or "gvpg", created if neither exists), which must be able to reach
# DIR: a root-only (0700) scratchpad is not reachable, so use a directory
# under /tmp there.
#
# Run the Postgres subtests with:
#   GRAPHVEIN_TEST_POSTGRES_URL=$(bash scripts/cloud-postgres.sh url) bash scripts/test.sh
#
# Bash 3.2 compatible, like every script in this repository.
set -euo pipefail

PG_MAJOR=17
if [ "${1:-}" = "--allow-16" ]; then
  # CI tests against 17; 16 is only a stopgap when 17 cannot be installed.
  PG_MAJOR=16
  shift
fi
action=${1:-}
DIR=${2:-${GV_PG_DIR:-/tmp/graphvein-pg}}
# The test database and role, named like CI's (.github/workflows/ci.yml).
DB_NAME=graphvein_test
DB_USER=graphvein
# A file that marks DIR as created by this script, so stop never deletes a
# directory it does not own.
MARKER=graphvein-cloud-postgres

die() { printf 'cloud-postgres: %s\n' "$*" >&2; exit 1; }
say() { printf 'cloud-postgres: %s\n' "$*" >&2; }

case "$action" in
  start | stop | url | status) ;;
  *) die "usage: cloud-postgres.sh [--allow-16] start|stop|url|status [DIR]" ;;
esac

BIN=/usr/lib/postgresql/$PG_MAJOR/bin
if [ ! -x "$BIN/initdb" ] || [ ! -x "$BIN/pg_ctl" ]; then
  if [ "$PG_MAJOR" = 17 ]; then
    die "PostgreSQL 17 binaries not found in $BIN; run scripts/cloud-setup.sh (pass --allow-16 to use 16 instead)"
  fi
  die "PostgreSQL $PG_MAJOR binaries not found in $BIN"
fi

case "$DIR" in
  /*) ;;
  *) die "DIR must be an absolute path: $DIR" ;;
esac
DIR=${DIR%/}
DATA=$DIR/data

# Never inside a Git working tree (the repository, a worktree or a corpus).
probe=$DIR
while [ ! -d "$probe" ]; do
  probe=$(dirname "$probe")
done
if git -C "$probe" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  die "$DIR is inside a Git working tree; use a scratch directory outside the repository"
fi

# The socket path must fit in sockaddr_un (107 bytes on Linux).
socket_path="$DIR/.s.PGSQL.65535"
if [ "${#socket_path}" -gt 107 ]; then
  die "DIR is too long for a Unix socket path (${#socket_path} > 107 bytes): $DIR"
fi

# Choose the unprivileged user: postgres refuses to run as root.
if [ "$(id -u)" -eq 0 ]; then
  if id postgres >/dev/null 2>&1; then
    PG_OS_USER=postgres
  else
    if ! id gvpg >/dev/null 2>&1; then
      useradd --system --no-create-home --shell /usr/sbin/nologin gvpg
      say "created system user gvpg"
    fi
    PG_OS_USER=gvpg
  fi
  as_pg() { runuser -u "$PG_OS_USER" -- "$@"; }
else
  PG_OS_USER=$(id -un)
  as_pg() { "$@"; }
fi

is_running() {
  [ -f "$DATA/PG_VERSION" ] && as_pg "$BIN/pg_ctl" -D "$DATA" status >/dev/null 2>&1
}

port() {
  cat "$DIR/port" 2>/dev/null || die "no cluster in $DIR (run start first)"
}

# Returns 0 when something accepts connections on 127.0.0.1:$1.
port_in_use() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

start() {
  if [ -e "$DIR" ] && [ ! -f "$DIR/$MARKER" ] && [ -n "$(ls -A "$DIR")" ]; then
    die "$DIR exists and is not empty; refusing to use it"
  fi
  if is_running; then
    say "already running in $DIR on 127.0.0.1:$(port)"
    return 0
  fi
  # Check before creating anything: the server user must reach DIR's parent.
  local parent
  parent=$(dirname "$DIR")
  while [ ! -d "$parent" ]; do
    parent=$(dirname "$parent")
  done
  if ! as_pg test -x "$parent"; then
    die "user $PG_OS_USER cannot reach $parent (root-only scratchpad?); use a directory under /tmp"
  fi
  mkdir -p "$DIR"
  touch "$DIR/$MARKER"
  chmod 0700 "$DIR"
  if [ "$(id -u)" -eq 0 ]; then
    chown "$PG_OS_USER" "$DIR"
  fi

  if [ ! -f "$DATA/PG_VERSION" ]; then
    # Trust is safe here: the server only listens on its own socket and
    # 127.0.0.1, and the cluster holds throwaway test data.
    as_pg "$BIN/initdb" -D "$DATA" -U "$DB_USER" --auth=trust -E UTF8 --locale=C.UTF-8 >"$DIR/initdb.log" 2>&1 ||
      die "initdb failed; see $DIR/initdb.log"
    # Replace the generated rules: local socket and IPv4 loopback only.
    printf 'local all all trust\nhost all all 127.0.0.1/32 trust\n' | as_pg tee "$DATA/pg_hba.conf" >/dev/null

    local p=54329
    while port_in_use "$p"; do
      p=$((p + 1))
      [ "$p" -le 54429 ] || die "no free port in 54329-54429"
    done
    # fsync and friends are off: speed matters, durability does not.
    as_pg tee -a "$DATA/postgresql.conf" >/dev/null <<EOF

# Added by scripts/cloud-postgres.sh
listen_addresses = '127.0.0.1'
port = $p
unix_socket_directories = '$DIR'
fsync = off
synchronous_commit = off
full_page_writes = off
EOF
    printf '%s\n' "$p" >"$DIR/port"
  fi

  as_pg "$BIN/pg_ctl" -D "$DATA" -l "$DIR/server.log" -w -t 60 start >/dev/null ||
    die "pg_ctl start failed; see $DIR/server.log"
  local p
  p=$(port)
  if ! as_pg "$BIN/psql" -h "$DIR" -p "$p" -U "$DB_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'" | grep -q 1; then
    as_pg "$BIN/createdb" -h "$DIR" -p "$p" -U "$DB_USER" "$DB_NAME"
  fi
  say "started PostgreSQL $("$BIN/postgres" --version | awk '{print $3}') as $PG_OS_USER in $DIR on 127.0.0.1:$p"
}

stop() {
  if [ ! -e "$DIR" ]; then
    say "nothing to stop: $DIR does not exist"
    return 0
  fi
  [ -f "$DIR/$MARKER" ] || die "$DIR was not created by this script; refusing to delete it"
  if is_running; then
    as_pg "$BIN/pg_ctl" -D "$DATA" -m fast -w -t 60 stop >/dev/null || die "pg_ctl stop failed"
  fi
  rm -rf "${DIR:?}"
  say "stopped and deleted $DIR"
}

case "$action" in
  start) start ;;
  stop) stop ;;
  url)
    is_running || die "no server running in $DIR (run start first)"
    # TCP rather than the socket: egress.OpenPostgres admits the parsed host
    # and port as the only database destination, as with CI's URL.
    printf 'postgres://%s@127.0.0.1:%s/%s?sslmode=disable\n' "$DB_USER" "$(port)" "$DB_NAME"
    ;;
  status)
    if is_running; then
      echo "running: $DIR, 127.0.0.1:$(port), socket dir $DIR"
    elif [ -e "$DIR" ]; then
      echo "stopped: $DIR exists"
      exit 3
    else
      echo "absent: $DIR"
      exit 3
    fi
    ;;
esac
