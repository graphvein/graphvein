# Joins the hard-wrapped lines of each Markdown paragraph, list item and
# block quote into one line, for text that GitHub shows with every line
# break kept: the export commit message and pull request description
# (scripts/export-public.sh). Blank lines, headings, fenced and indented
# code blocks, table rows, HTML blocks (up to the next blank line),
# thematic breaks and explicit hard breaks (two trailing spaces or a
# backslash) stay as they are; a list item's continuation lines join the
# item, and nested items stay separate.
# scripts/releasenotes does the same for release notes (in Go, with its own
# rules for comments and the title).
#
#   awk -f scripts/unwrap-markdown.awk FILE
#
# POSIX awk without interval expressions, so it runs with the awk of
# macOS, Ubuntu (mawk) and Git for Windows (gawk). Tested by
# scripts/test-export-public.sh.

function flush() {
  if (have) print cur
  cur = ""
  have = 0
  quote = 0
}

function hardbreak(s) {
  return s ~ /  $/ || s ~ /\\$/
}

function join(a, b) {
  sub(/[ \t]+$/, "", a)
  sub(/^[ \t]+/, "", b)
  return a " " b
}

# closes(line): the line closes the open code fence (the fence character
# only, at least as many as the opening fence).
function closes(line,   t, rest) {
  t = line
  sub(/^[ \t]+/, "", t)
  sub(/[ \t]+$/, "", t)
  if (length(t) < length(fence)) return 0
  rest = t
  gsub(substr(fence, 1, 1), "", rest)
  return rest == ""
}

BEGIN {
  have = 0
  quote = 0
  inlist = 0
  html = 0
  fence = ""
}

{
  line = $0
  sub(/\r$/, "", line)

  if (fence != "") {
    print line
    if (closes(line)) fence = ""
    next
  }

  if (line ~ /^[ \t]*$/) {
    flush()
    print ""
    html = 0
    next
  }

  # Inside an HTML block every line stays as it is.
  if (html) {
    print line
    next
  }

  if (line ~ /^[ \t]*(```|~~~)/) {
    flush()
    t = line
    sub(/^[ \t]+/, "", t)
    match(t, /^(`+|~+)/)
    fence = substr(t, 1, RLENGTH)
    print line
    next
  }

  # Indented code block: kept line by line.
  if (!have && !inlist && line ~ /^(    |\t)/) {
    print line
    next
  }

  # An HTML line that starts a block starts an HTML block.
  if (!have && line ~ /^[ \t]*</) {
    print line
    html = 1
    inlist = 0
    next
  }

  # Headings, thematic breaks, setext underlines and table rows stay on
  # their own line.
  if (line ~ /^ ? ? ?#+([ \t]|$)/ ||
      line ~ /^ ? ? ?(-[ \t]*-[ \t]*-[- \t]*|\*[ \t]*\*[ \t]*\*[* \t]*|_[ \t]*_[ \t]*_[_ \t]*)$/ ||
      line ~ /^ ? ? ?=+[ \t]*$/ ||
      line ~ /^[ \t]*\|/) {
    flush()
    print line
    inlist = 0
    next
  }

  # A list item starts a new line.
  if (line ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/) {
    flush()
    cur = line
    have = 1
    inlist = 1
    next
  }

  # Block quote: its lines join, without the repeated marker.
  if (line ~ /^ ? ? ?>/) {
    text = line
    sub(/^ *>/, "", text)
    if (have && quote && !hardbreak(cur)) {
      cur = join(cur, text)
    } else {
      flush()
      cur = line
      have = 1
      quote = 1
    }
    inlist = 0
    next
  }

  if (have && !hardbreak(cur)) {
    cur = join(cur, line)
    next
  }

  # A new paragraph, or the line after a hard break.
  flush()
  cur = line
  have = 1
  if (line !~ /^[ \t]/) inlist = 0
}

END {
  flush()
}
