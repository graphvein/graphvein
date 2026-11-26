package main

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"path"
	"strconv"
	"strings"
)

// PlannedRun is one (agent, repetition, mode, question) unit of the plan.
type PlannedRun struct {
	Agent    string
	Rep      int
	Mode     string
	Question string
}

// makePlan orders the runs: agents one after another; per repetition a
// seeded shuffle of all (question, mode) pairs, so the two modes of a
// question are not always adjacent. The same seed gives the same order on
// every machine.
func makePlan(agents, modes, questions []string, reps int, seed uint64, shuffle bool) []PlannedRun {
	return makePlanReps(agents, modes, questions, reps, nil, seed, shuffle)
}

// makePlanReps is makePlan with per-agent repetition counts: an agent named
// in override runs that many repetitions instead of reps (a subscription
// agent may be run with fewer repetitions to stay within its usage limits;
// the manifest records the counts).
func makePlanReps(agents, modes, questions []string, reps int, override map[string]int, seed uint64, shuffle bool) []PlannedRun {
	var out []PlannedRun
	for _, agent := range agents {
		n := reps
		if v, ok := override[agent]; ok {
			n = v
		}
		for rep := 1; rep <= n; rep++ {
			var block []PlannedRun
			for _, q := range questions {
				for _, mode := range modes {
					if !agentRunsMode(agent, mode) {
						continue
					}
					block = append(block, PlannedRun{Agent: agent, Rep: rep, Mode: mode, Question: q})
				}
			}
			if shuffle {
				h := fnv.New64a()
				h.Write([]byte(agent))
				r := rand.New(rand.NewPCG(seed, h.Sum64()+uint64(rep)))
				r.Shuffle(len(block), func(i, j int) { block[i], block[j] = block[j], block[i] })
			}
			out = append(out, block...)
		}
	}
	return out
}

// agentRunsMode says whether an agent id has runs in a mode. codex-visible
// exists only to show Codex its MCP tools, so it has no base mode; its
// comparison base is the codex agent's base runs.
func agentRunsMode(agent, mode string) bool {
	return !(agent == "codex-visible" && mode != "mcp")
}

// matchesAny reports whether a run id matches one of the patterns (path.Match
// syntax, e.g. "codex-visible-mcp-*"); no patterns match everything.
func matchesAny(id string, patterns []string) (bool, error) {
	if len(patterns) == 0 {
		return true, nil
	}
	for _, p := range patterns {
		ok, err := path.Match(p, id)
		if err != nil {
			return false, fmt.Errorf("--rerun pattern %q: %w", p, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// parseAgentReps reads "agent=n,agent=n"; an empty string gives no override.
func parseAgentReps(spec string) (map[string]int, error) {
	out := map[string]int{}
	for _, part := range splitList(spec) {
		name, num, ok := strings.Cut(part, "=")
		n, err := strconv.Atoi(num)
		if !ok || name == "" || err != nil || n < 1 {
			return nil, fmt.Errorf("--agent-reps: %q is not agent=N with N >= 1", part)
		}
		out[name] = n
	}
	return out, nil
}
