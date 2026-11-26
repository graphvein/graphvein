#!/usr/bin/env bash
# Offline rehearsal of the export and import tooling (ADR 0080, revision 1):
# scripts/export-public.sh, scripts/import-contrib.sh and
# scripts/check-public-privacy.sh. Everything happens in a temporary folder:
# a small working repository with a private and a public path, a local bare
# "public" repository and clones of it. Nothing touches the network.
#
# A small repository rather than this repository's own snapshot: the
# scripts only need the guard files, so a few files exercise every branch
# in seconds, and the result does not depend on the state of HEAD (the real
# snapshot is checked by TestPublicSnapshot).
#
# Usage: bash scripts/test-export-public.sh
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
src=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
t=$(mktemp -d)
trap 'rm -rf -- "$t"' EXIT
if command -v cygpath >/dev/null 2>&1; then
  t=$(cygpath -m "$t")
fi

# fail MESSAGE: stop, with the output of the last captured command (the
# checked commands write to $t/out.txt) so that a CI log shows why.
fail() {
  echo "test-export-public: FAIL: $*" >&2
  if [ -s "$t/out.txt" ]; then
    echo "test-export-public: output of the last captured command:" >&2
    sed 's/^/  | /' "$t/out.txt" >&2
  fi
  exit 1
}
# expect_fail TEXT CMD...: CMD must fail and print TEXT.
expect_fail() {
  local want=$1
  shift
  if "$@" > "$t/out.txt" 2>&1; then
    fail "accepted: $*"
  fi
  grep -qF -- "$want" "$t/out.txt" || fail "no '$want' from: $*"
}
# Hermetic repositories. Git for Windows' system configuration sets
# core.autocrlf=true, and these small repositories have no .gitattributes:
# a clone would check its files out with CRLF, and the next "git add -A"
# would commit every file as changed. So each repository gets
# core.autocrlf=false in its own (throwaway) configuration before anything
# is checked out: setup_repo right after git init, clone_repo for clones
# (git clone -c writes the setting before the checkout). The machine's
# configuration is never changed. An import under core.autocrlf=true is
# checked on purpose further down.
setup_repo() {
  # setup_repo DIR NAME EMAIL
  git -C "$1" config core.autocrlf false
  git -C "$1" config core.eol lf
  git -C "$1" config commit.gpgsign false
  git -C "$1" config push.negotiate false
  git -C "$1" config user.name "$2"
  git -C "$1" config user.email "$3"
}

# The working repository: the guard scripts, the internal and the public
# AGENTS.md, the Dependabot configuration under docs/public (the snapshot
# places it at .github/dependabot.yml), a private folder, public files and
# a private name pattern.
priv=$t/private
mkdir -p "$priv/scripts/private" "$priv/docs/public" "$priv/docs/perf" "$priv/src"
mkdir -p "$priv/scripts/hooks"
cp "$src/hooks/pre-push-public" "$priv/scripts/hooks/"
for f in export-public.sh import-contrib.sh public-snapshot.sh check-public-tree.sh check-public-privacy.sh \
  public-exclude.txt public-reference-exempt.txt public-privacy-exempt.txt export-pr-template.md \
  unwrap-markdown.awk; do
  cp "$src/$f" "$priv/scripts/"
done
echo 'zorblax  # test-only private name' > "$priv/scripts/private/privacy-patterns.txt"
# Split words keep this file clear of the reference scan.
printf '# Internal\n\nIgnore docs/%s.\n' "HAND""OFF.md" > "$priv/AGENTS.md"
printf '# AGENTS.md\n\nRead docs/VISION.md.\n' > "$priv/docs/public/AGENTS.md"
printf 'version: 2\nupdates: []\n' > "$priv/docs/public/dependabot.yml"
echo 'private timing notes' > "$priv/docs/perf/notes.md"
echo '# Demo' > "$priv/README.md"
printf 'package a\n' > "$priv/src/a.go"
git init -q -b main "$priv"
setup_repo "$priv" Maintainer maintainer@example.org
git -C "$priv" add -A
git -C "$priv" commit -qm 'Start'
clone_repo() {
  # clone_repo URL DIR NAME EMAIL
  git clone -q -c core.autocrlf=false -c core.eol=lf "$1" "$2" 2> /dev/null
  setup_repo "$2" "$3" "$4"
}
export_public() { bash "$priv/scripts/export-public.sh" "$@"; }
import_contrib() { bash "$priv/scripts/import-contrib.sh" "$@"; }

# The public repository and the clone the exports are made in.
bare=$t/public.git
git init -q --bare -b main "$bare"
pub=$t/public
clone_repo "$bare" "$pub" Maintainer maintainer@example.org
# The working repository's remote, and the public push guard in the
# maintainer's checkout (the working repository), matched to the bare
# "public" repository.
remote=$t/private.git
git init -q --bare -b main "$remote"
git -C "$priv" remote add origin "$remote"
mkdir -p "$t/hooks"
cp "$src/hooks/pre-push-public" "$t/hooks/pre-push"
chmod +x "$t/hooks/pre-push"
git -C "$priv" config core.hooksPath "$t/hooks"
git -C "$priv" config graphvein.publicRemotePattern 'public\.git$'
# publish BRANCH TARGET: the export commit goes from the clone to the
# working repository's remote, and from the maintainer's checkout to the
# public repository through the guard.
publish() {
  git -C "$pub" push -q "$remote" "refs/heads/$1:refs/heads/$1"
  git -C "$priv" fetch -q origin "$1"
  git -C "$priv" push -q "$bare" "FETCH_HEAD:refs/heads/$2"
  git -C "$pub" fetch -q origin
}

# A dirty working tree is refused.
echo changed >> "$priv/README.md"
expect_fail 'uncommitted changes' export_public --initial launch "$pub" 'First public snapshot'
git -C "$priv" checkout -q -- README.md
echo stray > "$priv/stray.txt"
expect_fail 'uncommitted changes or untracked files' export_public --initial launch "$pub" 'First public snapshot'
rm "$priv/stray.txt"

# The initial export: one commit without a parent, the public tree only.
export_public --initial launch "$pub" 'First public snapshot' > "$t/out.txt" 2>&1 || fail "initial export"
grep -qF "FETCH_HEAD:refs/heads/main" "$t/out.txt" || fail "no public push command for --initial"
grep -qF "push $remote refs/heads/export/launch:refs/heads/export/launch" "$t/out.txt" || fail "no push command to the working repository's remote"
c1=$(git -C "$pub" rev-parse export/launch)
[ "$(git -C "$pub" rev-list --parents -n 1 "$c1" | wc -w | tr -d ' ')" = 1 ] || fail "initial commit has a parent"
[ "$(git -C "$pub" ls-tree -r --name-only "$c1" | LC_ALL=C sort | tr '\n' ' ')" = \
  ".github/dependabot.yml AGENTS.md README.md scripts/check-public-privacy.sh scripts/check-public-tree.sh scripts/export-pr-template.md scripts/export-public.sh scripts/hooks/pre-push-public scripts/import-contrib.sh scripts/public-exclude.txt scripts/public-privacy-exempt.txt scripts/public-reference-exempt.txt scripts/public-snapshot.sh scripts/unwrap-markdown.awk src/a.go " ] ||
  fail "unexpected initial tree: $(git -C "$pub" ls-tree -r --name-only "$c1" | tr '\n' ' ')"
git -C "$pub" show "$c1:AGENTS.md" | grep -q VISION || fail "root AGENTS.md is not the public one"
[ "$(git -C "$pub" rev-parse "$c1:.github/dependabot.yml")" = "$(git -C "$priv" rev-parse HEAD:docs/public/dependabot.yml)" ] ||
  fail ".github/dependabot.yml is not docs/public/dependabot.yml"
# public-snapshot.sh places it the same way, and refuses a tree that holds
# both paths.
bash "$priv/scripts/public-snapshot.sh" "$t/snap" > "$t/out.txt" 2>&1 || fail "public-snapshot"
cmp -s "$t/snap/.github/dependabot.yml" "$priv/docs/public/dependabot.yml" || fail "the snapshot's .github/dependabot.yml"
[ ! -e "$t/snap/docs/public/dependabot.yml" ] || fail "the snapshot keeps docs/public/dependabot.yml"
mkdir -p "$priv/.github"
cp "$priv/docs/public/dependabot.yml" "$priv/.github/dependabot.yml"
git -C "$priv" add -A
git -C "$priv" commit -qm 'Both Dependabot paths'
expect_fail 'both docs/public/dependabot.yml and .github/dependabot.yml' bash "$priv/scripts/public-snapshot.sh" "$t/snap2"
expect_fail 'both docs/public/dependabot.yml and .github/dependabot.yml' export_public T008 "$pub" 'Both'
git -C "$priv" reset -q --hard HEAD~1
[ "$(git -C "$pub" log -1 --format='%an <%ae> %G?' "$c1")" = "Maintainer <maintainer@example.org> N" ] || fail "export author or signature"
[ "$(git -C "$pub" log -1 --format=%B "$c1" | sed "/^$/d" | tr "\n" "|")" = "First public snapshot|Task: launch|" ] || fail "export message"
[ -z "$(git -C "$pub" status --porcelain)" ] || fail "the export touched the clone's working tree"
expect_fail 'already has branch' export_public --initial launch "$pub" 'Again'
expect_fail 'already has commits' export_public --initial launch2 "$pub" 'Again'
publish export/launch main
# The guard refuses the working repository's own history (internal
# AGENTS.md, private paths).
expect_fail 'stopped at the first bad commit (1 of 1 to check)' git -C "$priv" push "$bare" main:refs/heads/leak
# The walk: a clean commit Y on top of a private commit X. Without the
# public remote's tracking refs every commit is checked (X is caught), and
# another remote's tracking refs are not trusted; with the public remote's
# tracking refs holding X, only Y is checked.
x=$(git -C "$priv" rev-parse main)
y=$(git -C "$priv" commit-tree "$(git -C "$pub" rev-parse "$c1^{tree}")" -p "$x" -m probe)
expect_fail 'stopped at the first bad commit (2 of 2 to check)' git -C "$priv" push "$bare" "$y:refs/heads/probe"
git -C "$priv" update-ref refs/remotes/origin/probe "$x"
expect_fail "commit $x contains private path docs/perf" git -C "$priv" push "$bare" "$y:refs/heads/probe"
git -C "$priv" update-ref -d refs/remotes/origin/probe
git -C "$priv" remote add public "$bare"
git -C "$priv" update-ref refs/remotes/public/probe "$x"
git -C "$priv" push -q "$bare" "$y:refs/heads/probe" || fail "the guard rechecked commits the public remote has"
git -C "$priv" push -q "$bare" :refs/heads/probe
git -C "$priv" update-ref -d refs/remotes/public/probe
git -C "$priv" remote remove public
export_public --check-merged origin/main "$pub" > "$t/out.txt" 2>&1 || fail "check-merged after the initial export"

# A contributor pull request, squash-merged on the public side.
con=$t/contributor
clone_repo "$bare" "$con" 'Pat Contributor' pat@example.com
echo 'More docs.' >> "$con/README.md"
printf 'package a\n\nfunc B() {}\n' > "$con/src/b.go"
git -C "$con" add -A
GIT_AUTHOR_DATE='1790000000 +0200' git -C "$con" commit -qm 'Add B (#7)'
git -C "$con" push -q origin HEAD:main
git -C "$pub" fetch -q origin
if ! import_contrib "$pub" origin/main 7 > "$t/out.txt" 2>&1; then
  {
    echo "test-export-public: the contributor commit changes:"
    git -C "$pub" diff --name-status origin/main^ origin/main | sed 's/^/  | /'
  } >&2
  fail "import"
fi
[ "$(git -C "$priv" log -1 --format='%an <%ae> %ad|%cn <%ce>' --date=raw import/pr-7)" = \
  "Pat Contributor <pat@example.com> 1790000000 +0200|Maintainer <maintainer@example.org>" ] || fail "import author or committer"
[ "$(git -C "$priv" log -1 --format=%B import/pr-7 | sed '/^$/d' | tr '\n' '|')" = "Add B (#7)|Public-PR: #7|" ] || fail "import message"
git -C "$priv" show import/pr-7:src/b.go | grep -q 'func B' || fail "import content"
[ "$(git -C "$priv" rev-parse HEAD)" = "$(git -C "$priv" rev-parse main)" ] && [ -z "$(git -C "$priv" status --porcelain)" ] ||
  fail "the import touched the working tree"
export_public --check-merged origin/main --private import/pr-7 "$pub" > "$t/out.txt" 2>&1 || fail "check-merged of the import"
expect_fail 'already exists' import_contrib "$pub" origin/main 7

# Windows-like configuration on purpose: a contributor and a maintainer
# with core.autocrlf=true (Git for Windows' default). The contributor's
# checkout holds CRLF files and Git stores LF; the import and the check
# read Git objects only, so they carry just the changed paths, with LF, and
# leave the maintainer's working tree alone. crlf CMD... adds the setting
# to GIT_CONFIG_* (Git 2.31+; entries already there are kept), so it
# applies to every Git command the scripts run.
win=$t/windows-contributor
git clone -q -c core.autocrlf=true "$bare" "$win" 2> /dev/null
setup_repo "$win" 'Win Contributor' win@example.com
git -C "$win" config core.autocrlf true
git -C "$win" config --unset core.eol
tr -d '\r' < "$win/README.md" | cmp -s - "$win/README.md" && fail "the core.autocrlf=true clone has no CRLF files"
printf 'Windows notes.\r\n' >> "$win/README.md"
printf 'package a\r\n\r\nfunc W() {}\r\n' > "$win/src/w.go"
git -C "$win" add -A 2> /dev/null
git -C "$win" commit -qm 'Add W (#12)' 2> /dev/null
git -C "$win" push -q origin HEAD:refs/heads/win
git -C "$pub" fetch -q origin
crlf() (
  n=${GIT_CONFIG_COUNT:-0}
  export "GIT_CONFIG_KEY_$n=core.autocrlf" "GIT_CONFIG_VALUE_$n=true" "GIT_CONFIG_COUNT=$((n + 1))"
  "$@"
)
crlf import_contrib --base import/pr-7 "$pub" origin/win 12 > "$t/out.txt" 2>&1 || fail "import with core.autocrlf=true"
[ "$(git -C "$priv" diff --name-only import/pr-7 import/pr-12 | tr '\n' ' ')" = "README.md src/w.go " ] ||
  fail "import with core.autocrlf=true changes: $(git -C "$priv" diff --name-only import/pr-7 import/pr-12 | tr '\n' ' ')"
for f in README.md src/w.go; do
  git -C "$priv" show "import/pr-12:$f" > "$t/blob.txt"
  tr -d '\r' < "$t/blob.txt" | cmp -s - "$t/blob.txt" || fail "import with core.autocrlf=true stored CR in $f"
done
[ -z "$(crlf git -C "$priv" status --porcelain)" ] || fail "the import with core.autocrlf=true touched the working tree"
crlf export_public --check-merged origin/win --private import/pr-12 "$pub" > "$t/out.txt" 2>&1 ||
  fail "check-merged with core.autocrlf=true"
git -C "$priv" update-ref -d refs/heads/import/pr-12

# Malicious or misplaced pull requests are refused.
git -C "$con" checkout -q -b bad
mkdir -p "$con/docs/perf"
echo 'leak' > "$con/docs/perf/x.md"
git -C "$con" add -A && git -C "$con" commit -qm 'Private path'
git -C "$con" push -q origin bad
echo 'other' > "$con/AGENTS.md"
git -C "$con" commit -qam 'Root AGENTS.md'
git -C "$con" push -q origin bad
git -C "$pub" fetch -q origin
expect_fail 'private path docs/perf/x.md' import_contrib "$pub" origin/bad~1 8
expect_fail 'root AGENTS.md' import_contrib "$pub" origin/bad 9
expect_fail 'private path docs/perf/x.md' import_contrib "$pub" origin/main..origin/bad 10
mkdir -p "$con/scripts/private"
echo x > "$con/scripts/private/p.txt"
git -C "$con" add -A && git -C "$con" commit -qm 'Private scripts'
git -C "$con" push -q origin bad
git -C "$pub" fetch -q origin
expect_fail 'private path scripts/private/p.txt' import_contrib "$pub" origin/bad 11
mkdir -p "$con/.github"
echo 'version: 2' > "$con/.github/dependabot.yml"
git -C "$con" add -A && git -C "$con" commit -qm 'Dependabot change'
git -C "$con" push -q origin bad
git -C "$pub" fetch -q origin
expect_fail 'apply that change to docs/public/dependabot.yml by hand' import_contrib "$pub" origin/bad 13
git -C "$priv" rev-parse -q --verify import/pr-8 > /dev/null && fail "a refused import left a branch"

# The second export: the import merged, a maintainer change and a deletion.
git -C "$priv" merge -q --ff-only import/pr-7
printf 'package a\n\nfunc C() {}\n' > "$priv/src/c.go"
git -C "$priv" rm -q src/a.go
git -C "$priv" add -A
git -C "$priv" commit -qm 'Add C'
# Hard-wrapped notes become one line per paragraph and per bullet in the
# commit message and in the pull request description.
printf '%s\n' 'Adds C and removes' 'a.go.' '' '- First bullet,' '  wrapped.' '- Second.' '' '' > "$t/notes.md"
export_public --body "$t/notes.md" T002 "$pub" 'Add C' > "$t/out.txt" 2>&1 || fail "second export"
grep -qF 'gh pr create' "$t/out.txt" || fail "no pull request command"
grep -qF 'Export of task T002' "$t/out.txt" || fail "no pull request text"
pr2=$(git -C "$pub" rev-parse --absolute-git-dir)/export-pr-T002.md
grep -qxF 'Adds C and removes a.go.' "$pr2" || fail "pull request notes are not unwrapped"
grep -qxF -- '- First bullet, wrapped.' "$pr2" || fail "pull request bullet is not unwrapped"
grep -q '^Export of task T002 from .* squashed commit whose tree .* after this task\.$' "$pr2" ||
  fail "pull request template text is not unwrapped"
c2=$(git -C "$pub" rev-parse export/T002)
[ "$(git -C "$pub" log -1 --format=%B "$c2" | tr '\n' '|')" = 'Add C||Adds C and removes a.go.||- First bullet, wrapped.|- Second.||Task: T002||' ] ||
  fail "export message: $(git -C "$pub" log -1 --format=%B "$c2" | tr '\n' '|')"
[ "$(git -C "$pub" rev-parse "$c2^")" = "$(git -C "$pub" rev-parse origin/main)" ] || fail "export parent is not the base"
git -C "$pub" cat-file -e "$c2:src/a.go" 2> /dev/null && fail "deletion not exported"
[ "$(git -C "$pub" diff --name-status origin/main "$c2" | tr '\t\n' ' |')" = "D src/a.go|A src/c.go|" ] ||
  fail "export diff: $(git -C "$pub" diff --name-status origin/main "$c2")"
expect_fail 'nothing to export' export_public --base "$c2" T003 "$pub" 'Same'
# The export reaches the public repository through the guard; the
# maintainer squash-merges it (here: the same tree) and checks.
publish export/T002 export/T002
git -C "$pub" push -q origin export/T002:main
git -C "$pub" fetch -q origin
export_public --check-merged origin/main "$pub" > "$t/out.txt" 2>&1 || fail "check-merged after the second export"

# A tree mismatch after the merge is reported, with the path.
git -C "$pub" checkout -q -b edited origin/main
echo 'edited in the merge' >> "$pub/README.md"
git -C "$pub" commit -qam 'Edited'
expect_fail 'M	README.md' export_public --check-merged edited "$pub"
git -C "$pub" checkout -q --detach origin/main

# Leaks are caught before anything is committed in the clone.
leak() {
  # leak FILE TEXT EXPECTED: commit TEXT in FILE, export must fail with EXPECTED.
  printf '%s\n' "$2" > "$priv/$1"
  git -C "$priv" add -A
  git -C "$priv" commit -qm leak
  expect_fail "$3" export_public T009 "$pub" 'Leak'
  git -C "$priv" reset -q --hard HEAD~1
  git -C "$pub" rev-parse -q --verify export/T009 > /dev/null && fail "a refused export left a branch"
  return 0
}
leak docs.md "See docs/ta""sks/T0""01-x.md." 'names an internal record'
leak docs.md 'Our corpus Zorblax.' 'names a private name'
leak docs.md 'Ask jane.doe@gmai''l.com.' 'outside the allowlist'
leak docs.md "Built in /home/use""r/project." 'names an internal path'
leak docs.md 'Built in D:\de''v\graphvein.' 'names an internal path'
expect_fail 'commit message or pull request text' export_public T009 "$pub" 'Zorblax notes'
expect_fail 'is this working repository' export_public T009 "$priv" 'Self'

# Allowed addresses and URL user information are not personal details.
mkdir -p "$t/ok"
printf '%s\n' 'contact@graphvein.com a@example.com x@y.example.invalid git@github.com:o/r.git' \
  'https://user:tok3n@github.com/o/r.git helper@2.child' > "$t/ok/a.txt"
bash "$priv/scripts/check-public-privacy.sh" "$t/ok" > "$t/out.txt" 2>&1 || fail "allowed addresses rejected"
# Without the private patterns file the name check is skipped, and says so.
echo 'zorblax' > "$t/ok/b.txt"
GV_PRIVACY_PATTERNS=$t/none bash "$priv/scripts/check-public-privacy.sh" "$t/ok" > "$t/out.txt" 2>&1 || fail "check without patterns failed"
grep -qF 'private names not checked' "$t/out.txt" || fail "no notice without the patterns file"

# The unwrapping on its own (scripts/unwrap-markdown.awk): paragraphs,
# bullets with continuation lines, nested and numbered items, quotes, hard
# breaks, headings, rules, tables, HTML, fenced and indented code and
# blank lines.
cat > "$t/wrapped.md" <<'EOF'
# Title stays

A paragraph that is
hard-wrapped over
three lines.

- A bullet
  with a continuation line.
  - A nested bullet
    continued.
- Another bullet
lazily continued.
1. A numbered item
   continued.
2) Another.

> A quote
> over two lines.

Line with a hard break  
next line kept.
Backslash break\
kept too.

Heading
=======

---

| a | b |
| - | - |
<details>
inside HTML
</details>

```sh
code stays
  as it is
```
~~~~
tilde fence
~~~
still in the fence
~~~~

    indented code
    stays

Last paragraph
ends here.
EOF
cat > "$t/unwrapped.md" <<'EOF'
# Title stays

A paragraph that is hard-wrapped over three lines.

- A bullet with a continuation line.
  - A nested bullet continued.
- Another bullet lazily continued.
1. A numbered item continued.
2) Another.

> A quote over two lines.

Line with a hard break  
next line kept. Backslash break\
kept too.

Heading
=======

---

| a | b |
| - | - |
<details>
inside HTML
</details>

```sh
code stays
  as it is
```
~~~~
tilde fence
~~~
still in the fence
~~~~

    indented code
    stays

Last paragraph ends here.
EOF
awk -f "$src/unwrap-markdown.awk" "$t/wrapped.md" > "$t/got.md"
if ! cmp -s "$t/got.md" "$t/unwrapped.md"; then
  diff "$t/unwrapped.md" "$t/got.md" > "$t/out.txt" || true
  fail "unwrap-markdown.awk output differs (diff want got)"
fi
# CRLF input gives LF output; a last line without a newline gets one.
printf 'a\r\nb\r\n' | awk -f "$src/unwrap-markdown.awk" > "$t/got.md"
[ "$(cat "$t/got.md")" = 'a b' ] || fail "CRLF input: $(cat "$t/got.md")"
printf 'a\nb' | awk -f "$src/unwrap-markdown.awk" > "$t/got.md"
[ "$(wc -l < "$t/got.md" | tr -d ' ')" = 1 ] && [ "$(cat "$t/got.md")" = 'a b' ] || fail "no final newline"

echo "test-export-public: ok"
