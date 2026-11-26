#!/usr/bin/env bash
# Replay a complete snapshot; invocation arguments are deliberately ignored.
set -euo pipefail
cat -- "${GV_REPLAY_STREAM:?set GV_REPLAY_STREAM to the snapshot file}"
