# Shared functions of the benchmark driver; sourced by run.sh.
# Bash 3.2 compatible (macOS) and Git Bash on Windows: no associative arrays,
# no mapfile, and possibly empty arrays expanded as ${arr[@]+"${arr[@]}"}.

log() {
  printf '[gvbench %s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2
}

die() {
  printf 'gvbench: %s\n' "$*" >&2
  exit 1
}

# native_path prints a path in the form native programs (the Go helper, the
# agent CLIs) understand: C:/... in Git Bash, unchanged elsewhere.
native_path() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  else
    printf '%s\n' "$1"
  fi
}

# abs_dir prints the absolute path of an existing directory.
abs_dir() {
  (cd "$1" 2>/dev/null && pwd) || die "not a directory: $1"
}

# check_exec checks that a CLI path given by flag or environment exists and
# is executable; CLIs are never looked up on PATH. It logs and fails, so the
# caller decides whether that stops one agent or the whole run.
check_exec() {
  local name=$1 path=$2 flag=$3 env=$4
  if [ -z "$path" ]; then
    log "$name: no path given; pass $flag or set $env"
    return 1
  fi
  if [ ! -f "$path" ] || [ ! -x "$path" ]; then
    log "$name: $path is not an executable file ($flag / $env)"
    return 1
  fi
}

# gv runs the built Go helper.
gv() {
  "$gvbench" "$@"
}

# capture_version prints the first line of `<bin> <args>` or "unknown".
capture_version() {
  local out
  if out=$("$@" 2>&1 </dev/null); then
    printf '%s\n' "$out" | head -n 1
  else
    printf 'unknown\n'
  fi
}

# --- OpenRouter key usage (the only network calls of the benchmark) --------
# The key is read only from OPENROUTER_API_KEY. It is passed to curl as a
# header on standard input (printf is a shell builtin, so it never appears in
# a process list), never written to a file, never logged.

or_get() {
  # or_get URL OUT: authenticated GET into OUT; returns curl's status.
  local url=$1 out=$2 rc=0
  printf 'Authorization: Bearer %s\n' "$OPENROUTER_API_KEY" |
    "$curl" -sS --fail --max-time 30 -H @- -o "$out" "$url" 2>"$out.err" || rc=$?
  if [ "$rc" != 0 ]; then
    log "OpenRouter request failed (curl exit $rc): $(gv redact <"$out.err" | head -n 1)"
  fi
  rm -f "$out.err"
  return "$rc"
}

# key_read OUT writes the GET /api/v1/key response (in stand-in mode, the
# stand-in key file) to OUT.
key_read() {
  if [ "$standin" = 1 ]; then
    cp "$GV_BENCH_STANDIN_KEYFILE" "$1"
    return 0
  fi
  or_get https://openrouter.ai/api/v1/key "$1"
}

# key_usage prints the key's usage, or fails.
key_usage() {
  local f=$work/key.json
  key_read "$f" || return 1
  gv keyusage --key-json "$(native_path "$f")"
}

# key_settle prints the usage once two reads $settle seconds apart agree,
# and "unsettled <last>" after 60 seconds.
key_settle() {
  local prev now waited=0
  prev=$(key_usage) || { printf 'unsettled 0\n'; return 0; }
  while :; do
    if [ "$settle" -gt 0 ]; then
      sleep "$settle"
      waited=$((waited + settle))
    fi
    now=$(key_usage) || now=$prev
    if [ "$now" = "$prev" ]; then
      printf '%s\n' "$now"
      return 0
    fi
    if [ "$waited" -ge 60 ]; then
      printf 'unsettled %s\n' "$now"
      return 0
    fi
    prev=$now
  done
}

# generation_providers TRANSCRIPT prints the provider names OpenRouter
# reports for the generation ids found in the transcript, comma-separated.
generation_providers() {
  local ids id names="" n=0 f
  [ "$standin" = 1 ] && return 0
  ids=$(gv genids "$(native_path "$1")") || return 0
  for id in $ids; do
    n=$((n + 1))
    [ "$n" -gt 20 ] && break
    f=$work/generation.json
    if or_get "https://openrouter.ai/api/v1/generation?id=$id" "$f"; then
      names="$names,$(gv provider "$(native_path "$f")" | head -n 1)"
    fi
  done
  printf '%s\n' "${names#,}"
}

# float_sub A B prints A - B (awk does the arithmetic bash cannot).
float_sub() {
  awk -v a="$1" -v b="$2" 'BEGIN { printf "%.6f\n", a - b }'
}
