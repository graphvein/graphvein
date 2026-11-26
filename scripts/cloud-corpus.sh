#!/usr/bin/env bash
# Clone the public corpus checkouts for a Claude Code cloud session.
#
# Cloud sessions only: on the developer's machine the corpora already exist
# (and include a private checkout, which is not cloned here).
#
# Usage:
#   bash scripts/cloud-corpus.sh [NAME...]
#
# Clones each corpus (all thirty-four by default) into spike/parsing/.corpus/<name>
# at its pin, then checks HEAD and that `git status --porcelain --ignored` is
# empty. A checkout already at its pin and clean is skipped, so running this
# again is a no-op. A checkout at another commit or with changes is reported
# and left alone: it is never deleted. GV_CORPUS_DIR overrides the target
# directory (for trying the script outside the repository).
#
# Bash 3.2 compatible, like every script in this repository.
set -u

# name|GitHub repository|full commit. The corpus pin table in the private
# notes is the source of truth for the pins; update this list when a pin moves there.
# T021's four pins (Java, C# and Dart, plus commons-lang as a second Java
# oracle corpus, the T021 task record (internal)) follow localsend; then
# T024's Next.js and Supabase pins (the T024 task record (internal)) and T023's
# Go HTTP router pins dozzle, miniflux and owncast (the T023 task record (internal)).
PINS="
express|kenyipp/realworld-nodejs-example-app|94a864e4e458a5e0ae2b07d3cf762791769e8690
fastapi|fastapi/full-stack-fastapi-template|1762adac607a1b29cfc4da129557780beea71616
axum|launchbadge/realworld-axum-sqlx|f1b25654773228297e35c292f357d33b7121a101
axum-ddd|CaptainCollie/realworld-rust-axum-sqlx-ddd|a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2
react|yurisldk/realworld-react-fsd|969709a379b13935b4e1caae0ad8cad548e5879a
angular|realworld-apps/angular-realworld-example-app|dd99ed2cf39c805d719f943c5d7061a5683d98a8
angular-ngmodule|realworld-apps/angular-realworld-example-app|cd5e9c8f7b9b359e1d8e2b3249636e8c0bb7168b
langflow|langflow-ai/langflow|504c02fc47e76087b82b0e7cbe4186e9cdd916d4
realworld|gothinkster/realworld|ebbcdeb8d55b42a3a613c787560498b8ef10003f
kftray|hcavarsan/kftray|e3116e0cafa55d617f3f2ea0c4a0eb3c96845f2b
umami|umami-software/umami|559e3c607c23d2c72d9bd4f6481bda428255323c
hoppscotch|hoppscotch/hoppscotch|55908a76a1d0555c9bbb5d74244c14173df95b06
kaneo|usekaneo/kaneo|b832ab6ab2feac06e3e2a1bca91a468f11418fc5
dokploy|dokploy/dokploy|3c6d5f611389fdb0eeab9e13285ae3d6828800c0
opencollective-api|opencollective/opencollective-api|400e72955277f180bd21ff7e1f937efd3fe92de7
gladys|GladysAssistant/Gladys|52fd526a77b6df7e9e9e60bc7408a40418e127c3
seerr|seerr-team/seerr|8331f36c919cf4af79ece4d308434fd8d7002390
nest-admin|buqiyuan/nest-admin|5b549e540d52d04f08d26ded192a455a8e4c058d
maintainerr|Maintainerr/Maintainerr|3a19696dcb3bf878672dbb1c370081a600df3f9a
aqualink-app|aqualinkorg/aqualink-app|571ba33fc5d5906ed70c81eaa1aa4b3fd3857b50
crates.io|rust-lang/crates.io|4e6ac5a46387d41656746507e0b6d7b5d863cddf
gotify|gotify/server|5ce7f0ac9ecf9b59d8dfbef2835cd5719ae50a73
realworld-gin|gothinkster/golang-gin-realworld-example-app|a41289b2a4951e7cc09066f86742a27f85121c9b
spring-modulith|spring-projects/spring-modulith|a58dd45b07e8bf3983aba80744abedf3bc5a2abf
eshop|dotnet/eShop|913acea66708a59595351174cee9b29f37179d29
localsend|localsend/localsend|2e7ef1972b0b4e210608ec77ef5fad334f798f7e
commons-lang|apache/commons-lang|e2a0789bc936783fa2a75263412e3359d4c0f97b
chatbot-ui|mckaywrigley/chatbot-ui|81328b61d2a4ab597a7a057be70e785cf756d9f8
ai-chatbot|vercel/ai-chatbot|a8989b522fa3672a2ae52f053105e40993b36ca1
saas-starter|nextjs/saas-starter|6e33e58b1e553a41fe22e6b941a7229a002de361
supabase-stripe|KolbySisk/next-supabase-stripe-starter|9839fd47d282b1b41a80fb82efea80ac7d1080bc
dozzle|amir20/dozzle|748840bf99fced531aad9ab05497a63f38f79f61
miniflux|miniflux/v2|b31f7867f4a1ad9b4abfcebd376c94cecf5f1942
owncast|owncast/owncast|b2c4e69553f87884c856a1a71147fe8138d21cc3
"

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CORPUS=${GV_CORPUS_DIR:-$repo_root/spike/parsing/.corpus}
mkdir -p "$CORPUS" || exit 1
results=$(mktemp -d) || exit 1
trap 'rm -rf "$results"' EXIT

# Prints "<name>: <result>" and returns non-zero on failure.
checkout() {
  local name=$1 repo=$2 sha=$3
  local dir="$CORPUS/$name"
  if [ -e "$dir" ]; then
    local head dirty
    head=$(git -C "$dir" rev-parse HEAD 2>/dev/null || echo none)
    dirty=$(git -C "$dir" status --porcelain --ignored 2>/dev/null | wc -l | tr -d ' ')
    if [ "$head" = "$sha" ] && [ "$dirty" = 0 ]; then
      echo "$name: ok, already at ${sha:0:8} and clean"
      return 0
    fi
    echo "$name: FAILED, existing checkout is at $head with $dirty status lines (want $sha, clean); left untouched"
    return 1
  fi

  # Clone into a temporary name and rename at the end, so an interrupted run
  # never leaves a half-made checkout that a later run would have to judge.
  local tmp="$CORPUS/.$name.partial"
  rm -rf "${tmp:?}"
  git init -q "$tmp" &&
    git -C "$tmp" remote add origin "https://github.com/$repo.git" || {
    echo "$name: FAILED, git init"
    return 1
  }
  # GitHub serves a commit by its full hash; a depth-1 fetch of just that
  # commit is the smallest download. If the server refuses, fetch the
  # branch history without trees first so the commit becomes known.
  if ! git -C "$tmp" fetch -q --depth 1 origin "$sha" 2>/dev/null; then
    if ! git -C "$tmp" fetch -q --filter=tree:0 origin '+refs/heads/*:refs/remotes/origin/*' ||
      ! git -C "$tmp" fetch -q --depth 1 origin "$sha"; then
      rm -rf "${tmp:?}"
      echo "$name: FAILED, could not fetch $sha from $repo"
      return 1
    fi
  fi
  git -C "$tmp" checkout -q FETCH_HEAD || {
    rm -rf "${tmp:?}"
    echo "$name: FAILED, checkout"
    return 1
  }
  local head dirty
  head=$(git -C "$tmp" rev-parse HEAD)
  dirty=$(git -C "$tmp" status --porcelain --ignored | wc -l | tr -d ' ')
  if [ "$head" != "$sha" ] || [ "$dirty" != 0 ]; then
    rm -rf "${tmp:?}"
    echo "$name: FAILED, fresh clone is at $head with $dirty status lines"
    return 1
  fi
  mv "$tmp" "$dir" || {
    echo "$name: FAILED, rename"
    return 1
  }
  echo "$name: cloned at ${sha:0:8}, clean"
}

# Refuse unknown names rather than silently cloning less than asked.
for arg in "$@"; do
  case "$PINS" in
    *"
$arg|"*) ;;
    *) echo "cloud-corpus: unknown corpus: $arg" >&2; exit 1 ;;
  esac
done

# Each checkout is an independent directory, so they run in parallel; each
# job writes its own result file, read back in list order.
names=""
pids=""
for line in $PINS; do
  name=${line%%|*}
  rest=${line#*|}
  repo=${rest%%|*}
  sha=${rest#*|}
  if [ "$#" -gt 0 ]; then
    wanted=no
    for arg in "$@"; do
      [ "$arg" = "$name" ] && wanted=yes
    done
    [ "$wanted" = yes ] || continue
  fi
  checkout "$name" "$repo" "$sha" >"$results/$name" 2>&1 &
  names="$names $name"
  pids="$pids $!"
done

if [ -z "$names" ]; then
  echo "cloud-corpus: no corpus matches: $*" >&2
  exit 1
fi

failed=0
for pid in $pids; do
  wait "$pid" || failed=1
done
for name in $names; do
  cat "$results/$name"
done
if [ "$failed" -ne 0 ]; then
  echo "cloud-corpus: some checkouts failed (see above)" >&2
  exit 1
fi
echo "cloud-corpus: all checkouts at their pins and clean (private checkouts are not cloned)"
