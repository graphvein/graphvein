#!/usr/bin/env bash
# Fails when a tree that is meant to be public holds personal or private
# details (ADR 0080, revision 1). Run it on a snapshot made by
# scripts/public-snapshot.sh (scripts/export-public.sh does), or on the
# public repository's checkout.
#
# Usage: bash scripts/check-public-privacy.sh DIR
#
# Checks, on every text file under DIR:
#   email    e-mail addresses, except the allowlist below;
#   path     internal paths: developer and container home folders, session
#            scratch folders and local Windows development folders;
#   private  private corpus and project names, read from the private file
#            scripts/private/privacy-patterns.txt (one extended regular
#            expression per line, matched case-insensitively). That file
#            never enters the public tree, so the names it guards are not
#            published by the guard itself. Without it (a public checkout)
#            this check is skipped and the script says so.
#            GV_PRIVACY_PATTERNS names another patterns file (tests).
#
# scripts/public-privacy-exempt.txt lists reviewed exemptions as
# "CHECK PATH # reason" (CHECK is email or path); private names are never
# exempt.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: bash scripts/check-public-privacy.sh DIR" >&2
  exit 2
fi
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
dir=$1
patterns=${GV_PRIVACY_PATTERNS:-$here/private/privacy-patterns.txt}
exempt=$here/public-privacy-exempt.txt
if [ ! -d "$dir" ]; then
  echo "check-public-privacy: $dir is not a directory" >&2
  exit 2
fi
fail=0

# An address that is not personal:
#   contact@graphvein.com  the project's public contact (SECURITY.md, the
#                          CLA privacy notice);
#   git@github.com         the SSH user of GitHub remote URLs in tests;
#   example.com/.org/.net and the .example, .invalid and .test top-level
#   domains are reserved for documentation and tests (RFC 2606, RFC 6761);
#   tests and fixtures use them for authors and URLs.
allow='^(contact@graphvein\.com|git@github\.com|[^@]+@(([A-Za-z0-9-]+\.)*example\.(com|org|net)|([A-Za-z0-9-]+\.)*(example|invalid|test)))$'

# An address: the local part must not follow a character that makes it
# part of a URL's user information ("https://user:token@host") or of an
# escape ("\n@app.get"), and the domain starts with a letter (qualified
# names such as "helper@2.child" are not addresses).
email='(^|[^A-Za-z0-9._%+:/\\-])[A-Za-z0-9._%+-]+@[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}'

# Internal paths. Bracket expressions keep this file from matching itself.
paths='/home/use[r]/|/tmp/s[0-9][0-9]([^A-Za-z0-9]|$)|/tmp/gvp[g]-|/tmp/claude-[0-9]|/scratchpa[d]/|(^|[^A-Za-z0-9])[A-Za-z]:[\\/]+dev[\\/]|(^|[^A-Za-z0-9_.])/[a-z]/dev/'

exempt_entries=''
if [ -f "$exempt" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    entry=${line%%#*}
    entry=$(printf '%s' "$entry" | sed 's/[[:space:]]*$//; s/^[[:space:]]*//; s/[[:space:]][[:space:]]*/ /')
    [ -n "$entry" ] && exempt_entries="$exempt_entries
$entry"
  done < "$exempt"
fi

is_exempt() {
  # is_exempt CHECK PATH
  printf '%s\n' "$exempt_entries" | grep -qxF -- "$1 $2"
}

grep_tree() {
  # grep_tree FLAGS PATTERN: matching files under DIR, relative and sorted.
  (cd "$dir" && grep -rIl"$1" --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=.corpus -- "$2" . 2>/dev/null | sed 's|^\./||' | LC_ALL=C sort) || true
}

# 1. E-mail addresses.
for f in $(grep_tree E "$email"); do
  is_exempt email "$f" && continue
  bad=$(cd "$dir" && grep -oE -- "$email" "$f" | sed 's/^[^A-Za-z0-9._%+-]//' | grep -viE -- "$allow" | LC_ALL=C sort -u || true)
  if [ -n "$bad" ]; then
    echo "check-public-privacy: $f holds e-mail addresses outside the allowlist:" >&2
    printf '%s\n' "$bad" | head -5 | sed 's/^/  /' >&2
    fail=1
  fi
done

# 2. Internal paths.
for f in $(grep_tree E "$paths"); do
  is_exempt path "$f" && continue
  echo "check-public-privacy: $f names an internal path:" >&2
  (cd "$dir" && grep -nE -- "$paths" "$f" | head -3 | cut -c1-160 | sed 's/^/  /' >&2) || true
  fail=1
done

# 3. Private names.
if [ -f "$patterns" ]; then
  n=0
  while IFS= read -r line || [ -n "$line" ]; do
    entry=${line%%#*}
    entry=$(printf '%s' "$entry" | sed 's/[[:space:]]*$//; s/^[[:space:]]*//')
    [ -z "$entry" ] && continue
    n=$((n + 1))
    for f in $(grep_tree iE "$entry"); do
      # The hit is not printed: the output may end up in public CI logs.
      echo "check-public-privacy: $f names a private name (pattern $n of the private list)" >&2
      fail=1
    done
  done < "$patterns"
else
  echo "check-public-privacy: no private patterns file; private names not checked (e-mail and path checks only)"
fi

if [ "$fail" != 0 ]; then
  exit 1
fi
echo "check-public-privacy: $dir is clean"
