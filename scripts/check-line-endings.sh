#!/usr/bin/env bash
set -euo pipefail
# Git classifies binary content and honors binary attributes. Check tracked files
# in the current repository, including extensionless text and mixed line endings.
failed=0
entries=$(git ls-files --eol)
while IFS= read -r line; do
  if [[ "$line" =~ w/(crlf|mixed) ]] && [[ "$line" != *"attr/-text"* ]]; then
    path=${line#*$'\t'}
    case "$path" in
      *.bat|*.cmd|*.ps1) continue ;;
    esac
    printf 'CRLF line endings: %s\n' "$path" >&2
    failed=1
  fi
done <<< "$entries"
exit "$failed"
