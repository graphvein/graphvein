package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// GitConfig lists the -c options every git subprocess Graphvein runs gets
// (ADR 0056). Graphvein only reads a user's repository, so each option
// switches off something git may otherwise do on its own:
//   - protocol.allow=never: no transport, so nothing is fetched.
//   - maintenance.auto=false and gc.auto=0: no automatic maintenance or
//     gc --auto (repacking, commit-graph, pruning, gc.log).
//   - core.fsmonitor=false: no file system monitor daemon or index token.
//
// safe.directory and -C are added per call by GitCommand.
var GitConfig = []string{
	"-c", "protocol.allow=never",
	"-c", "maintenance.auto=false",
	"-c", "gc.auto=0",
	"-c", "core.fsmonitor=false",
}

// GitEnvironment is added to the environment of every git subprocess.
// GIT_OPTIONAL_LOCKS=0 stops read commands (status, diff, rev-parse with
// an index) from taking index.lock to refresh the index's stat data;
// GIT_NO_LAZY_FETCH=1 makes a partial clone's missing object an error
// instead of a fetch; GIT_TERMINAL_PROMPT=0 forbids credential prompts.
var GitEnvironment = []string{"GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"}

// GitCommand returns a git subprocess that runs args in the repository at
// dir with GitConfig and GitEnvironment. It is the only way Graphvein
// starts git (a test checks the source for other calls), so the read-only
// rule holds for every invocation. An empty dir runs git without -C, for
// commands such as init that name their own directory.
func GitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := append([]string{}, GitConfig...)
	if dir != "" {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			absolute = dir
		}
		full = append(full, "-c", "safe.directory="+filepath.ToSlash(absolute), "-C", absolute)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), GitEnvironment...)
	return cmd
}
