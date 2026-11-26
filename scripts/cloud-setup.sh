#!/usr/bin/env bash
# Environment setup for Claude Code cloud sessions working on Graphvein.
#
# Linux only (Ubuntu 24.04 "noble" cloud containers). Never run this on the
# developer's Windows machine: it installs system packages as root.
#
# Paste the whole file into the cloud environment's "setup script" setting.
# It is self-contained on purpose: the setup script may run before the
# repository is cloned, so it reads no repository files. It is idempotent:
# a second run checks each step and changes nothing.
#
# Steps (required steps make the script exit non-zero when they fail; an
# optional step only prints a warning, and later steps still run):
#   1. PostgreSQL 17 binaries from apt.postgresql.org (required). No cluster
#      is created and no server is started: tests start a throwaway cluster
#      in scratch with scripts/cloud-postgres.sh (AGENTS.md exception).
#   2. Node 24.22.2 from nodejs.org into /opt/node-v24.22.2 (required),
#      checked against SHASUMS256.txt. The system node is left alone.
#   3. Prefetch of the Go toolchain named in go.mod (optional), so the first
#      go command of the session does not stall on a download.
#
# Bash 3.2 compatible, like every script in this repository.
set -u

# Keep in sync with web/.nvmrc.
NODE_VERSION=24.22.2
# Keep in sync with the toolchain directive in go.mod.
GO_TOOLCHAIN=go1.27.4
PG_MAJOR=17
# Fingerprint of the apt.postgresql.org signing key (ACCC4CF8). The key is
# downloaded over HTTPS and must match this before apt trusts it.
PGDG_KEY_FPR=B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8
# www.postgresql.org (the usual key URL) is blocked by the session proxy;
# apt.postgresql.org serves the same key.
PGDG_KEY_URL=https://apt.postgresql.org/pub/repos/apt/ACCC4CF8.asc
PGDG_KEY_FILE=/etc/apt/keyrings/apt.postgresql.org.asc
PGDG_SOURCES=/etc/apt/sources.list.d/pgdg.sources

failed_required=""
failed_optional=""

say() { printf '[cloud-setup] %s\n' "$*"; }

# Run as root directly, otherwise through sudo (the cloud container runs as
# root; sudo keeps the script usable in a similar container that does not).
if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
elif command -v sudo >/dev/null 2>&1; then
  SUDO="sudo"
else
  say "FAILED: needs root or sudo"
  exit 1
fi

# --- Step 1: PostgreSQL 17 -------------------------------------------------

pg_installed() {
  [ -x "/usr/lib/postgresql/$PG_MAJOR/bin/initdb" ] &&
    dpkg-query -W -f='${Status}' "postgresql-$PG_MAJOR" 2>/dev/null | grep -q 'install ok installed'
}

# Lists PostgreSQL server processes, so the step can prove it started none.
pg_processes() {
  ps -eo pid=,comm= | awk '$2 == "postgres" || $2 == "postmaster" { print }'
}

install_postgres() {
  if pg_installed; then
    say "PostgreSQL $PG_MAJOR: already installed ($("/usr/lib/postgresql/$PG_MAJOR/bin/postgres" --version))"
  else
    # Without this, the package creates and starts a "main" cluster.
    # postgresql-common reads the file during install, so it must exist first.
    $SUDO mkdir -p /etc/postgresql-common || return 1
    if ! grep -q '^create_main_cluster = false' /etc/postgresql-common/createcluster.conf 2>/dev/null; then
      printf 'create_main_cluster = false\n' | $SUDO tee -a /etc/postgresql-common/createcluster.conf >/dev/null || return 1
      say "PostgreSQL: set create_main_cluster = false in /etc/postgresql-common/createcluster.conf"
    fi

    local tmp
    tmp=$(mktemp -d) || return 1
    if ! curl -fsSL --retry 3 -o "$tmp/key.asc" "$PGDG_KEY_URL"; then
      say "PostgreSQL: could not download the signing key from $PGDG_KEY_URL"
      rm -rf "$tmp"
      return 1
    fi
    # Accept the key only if it is exactly the published apt.postgresql.org key.
    local fpr
    fpr=$(gpg --show-keys --with-colons "$tmp/key.asc" 2>/dev/null | awk -F: '$1 == "fpr" { print $10; exit }')
    if [ "$fpr" != "$PGDG_KEY_FPR" ]; then
      say "PostgreSQL: signing key fingerprint mismatch (got '$fpr', want $PGDG_KEY_FPR)"
      rm -rf "$tmp"
      return 1
    fi
    $SUDO install -d -m 0755 /etc/apt/keyrings &&
      $SUDO install -m 0644 "$tmp/key.asc" "$PGDG_KEY_FILE" || { rm -rf "$tmp"; return 1; }
    rm -rf "$tmp"

    local arch
    arch=$(dpkg --print-architecture) || return 1
    printf 'Types: deb\nURIs: https://apt.postgresql.org/pub/repos/apt\nSuites: noble-pgdg\nComponents: main\nArchitectures: %s\nSigned-By: %s\n' \
      "$arch" "$PGDG_KEY_FILE" | $SUDO tee "$PGDG_SOURCES" >/dev/null || return 1
    say "PostgreSQL: wrote $PGDG_SOURCES (noble-pgdg, signed by $PGDG_KEY_FILE)"

    # policy-rc.d exit code 101 tells maintainer scripts not to start any
    # service, so the install leaves no server running (the image's existing
    # PostgreSQL 16 cluster stays down as well). Any existing policy is kept
    # aside and restored.
    local policy=/usr/sbin/policy-rc.d saved=""
    if [ -e "$policy" ]; then
      saved="$policy.cloud-setup-saved"
      $SUDO mv "$policy" "$saved" || return 1
    fi
    printf '#!/bin/sh\nexit 101\n' | $SUDO tee "$policy" >/dev/null && $SUDO chmod 0755 "$policy"

    local status=0
    say "PostgreSQL: apt-get update and install postgresql-$PG_MAJOR"
    $SUDO apt-get update -q >/dev/null || status=1
    if [ "$status" -eq 0 ]; then
      # --force-confold keeps the createcluster.conf edited above when
      # postgresql-common is upgraded from apt.postgresql.org.
      $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends \
        -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold \
        "postgresql-$PG_MAJOR" "postgresql-client-$PG_MAJOR" >/dev/null || status=1
    fi

    $SUDO rm -f "$policy"
    if [ -n "$saved" ]; then
      $SUDO mv "$saved" "$policy"
    fi
    [ "$status" -eq 0 ] || return 1
    pg_installed || return 1
    say "PostgreSQL $PG_MAJOR: installed ($("/usr/lib/postgresql/$PG_MAJOR/bin/postgres" --version))"
  fi

  # Verify on every run: no cluster for this version, no server running.
  if [ -d "/etc/postgresql/$PG_MAJOR" ] && [ -n "$(ls -A "/etc/postgresql/$PG_MAJOR" 2>/dev/null)" ]; then
    say "PostgreSQL: unexpected cluster configuration in /etc/postgresql/$PG_MAJOR"
    return 1
  fi
  local running
  running=$(pg_processes)
  if [ -n "$running" ]; then
    say "PostgreSQL: server processes are running:"
    printf '%s\n' "$running"
    return 1
  fi
  say "PostgreSQL: verified no $PG_MAJOR cluster exists and no server is running"
}

# --- Step 2: Node -----------------------------------------------------------

install_node() {
  local prefix="/opt/node-v$NODE_VERSION"
  if [ -x "$prefix/bin/node" ] && [ "$("$prefix/bin/node" --version 2>/dev/null)" = "v$NODE_VERSION" ]; then
    say "Node: v$NODE_VERSION already in $prefix"
    return 0
  fi
  local arch
  case "$(uname -m)" in
    x86_64) arch=x64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) say "Node: unsupported machine $(uname -m)"; return 1 ;;
  esac
  local name="node-v$NODE_VERSION-linux-$arch"
  local base="https://nodejs.org/dist/v$NODE_VERSION"
  local tmp
  tmp=$(mktemp -d) || return 1
  if ! curl -fsSL --retry 3 -o "$tmp/SHASUMS256.txt" "$base/SHASUMS256.txt" ||
    ! curl -fsSL --retry 3 -o "$tmp/$name.tar.xz" "$base/$name.tar.xz"; then
    say "Node: download from $base failed"
    rm -rf "$tmp"
    return 1
  fi
  # Check the tarball against its line in SHASUMS256.txt; refuse when the
  # line is missing, so a renamed file cannot skip the check.
  if ! grep "  $name.tar.xz\$" "$tmp/SHASUMS256.txt" >"$tmp/expected" || ! (cd "$tmp" && sha256sum -c --quiet expected); then
    say "Node: sha256 check of $name.tar.xz failed"
    rm -rf "$tmp"
    return 1
  fi
  # Unpack beside the target and move into place, so an interrupted run never
  # leaves a half-extracted $prefix that the check above would trust.
  $SUDO rm -rf "$prefix.partial" &&
    $SUDO mkdir -p "$prefix.partial" &&
    $SUDO tar -xJf "$tmp/$name.tar.xz" -C "$prefix.partial" --strip-components=1 --no-same-owner &&
    $SUDO rm -rf "$prefix" &&
    $SUDO mv "$prefix.partial" "$prefix" || { rm -rf "$tmp"; return 1; }
  rm -rf "$tmp"
  say "Node: installed $("$prefix/bin/node" --version) in $prefix (sha256 verified; system node unchanged)"
  say "Node: use it with: export PATH=$prefix/bin:\$PATH"
}

# --- Step 3: Go toolchain ---------------------------------------------------

prefetch_go() {
  local gobin
  gobin=$(command -v go 2>/dev/null || true)
  if [ -z "$gobin" ] && [ -x /usr/local/go/bin/go ]; then
    gobin=/usr/local/go/bin/go
  fi
  if [ -z "$gobin" ]; then
    say "Go: no go command found"
    return 1
  fi
  local out
  if out=$(GOTOOLCHAIN=$GO_TOOLCHAIN "$gobin" version 2>&1); then
    say "Go: $out"
    return 0
  fi
  # The session's NO_PROXY can list proxy.golang.org, which makes go try a
  # direct connection that DNS cannot resolve. Retry through the proxy.
  if out=$(NO_PROXY=localhost,127.0.0.1 no_proxy=localhost,127.0.0.1 GOTOOLCHAIN=$GO_TOOLCHAIN "$gobin" version 2>&1); then
    say "Go: $out (fetched with NO_PROXY=localhost,127.0.0.1)"
    return 0
  fi
  say "Go: $out"
  return 1
}

# --- Run --------------------------------------------------------------------

run_step() {
  local kind=$1 label=$2 fn=$3
  say "== $label"
  if "$fn"; then
    return 0
  fi
  if [ "$kind" = required ]; then
    say "FAILED (required): $label"
    failed_required="$failed_required; $label"
  else
    say "FAILED (optional, continuing): $label"
    failed_optional="$failed_optional; $label"
  fi
}

run_step required "PostgreSQL $PG_MAJOR binaries (no cluster, no service)" install_postgres
run_step required "Node $NODE_VERSION in /opt/node-v$NODE_VERSION" install_node
run_step optional "Go toolchain $GO_TOOLCHAIN prefetch" prefetch_go

if [ -n "$failed_optional" ]; then
  say "optional steps failed: ${failed_optional#; }"
fi
if [ -n "$failed_required" ]; then
  say "required steps failed: ${failed_required#; }"
  exit 1
fi
say "done"
