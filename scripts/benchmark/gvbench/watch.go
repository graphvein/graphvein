package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// ExecResult is what the driver observed about one CLI process. The driver
// (bash) starts and kills the process; wall time is measured from just before
// start to exit, the same way for every agent.
type ExecResult struct {
	ExitCode   int     `json:"exit_code"`
	WallMS     int64   `json:"wall_ms"`
	TimedOut   bool    `json:"timed_out"`
	CostCapped bool    `json:"cost_capped"`
	WatchedUSD float64 `json:"watched_worst_usd"`
	StartedAt  string  `json:"started_at"`
	StartError string  `json:"start_error,omitempty"`
}

// watchState is the incremental position of the live cost watch, kept in a
// small file between polls so each poll reads only new transcript bytes.
type watchState struct {
	Offset  int64
	Watched float64
	// LastID is the message id of the last Claude assistant event priced.
	LastID string
}

func readWatchState(path string) watchState {
	var s watchState
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	f := strings.Fields(string(data))
	if len(f) >= 2 {
		s.Offset, _ = strconv.ParseInt(f[0], 10, 64)
		s.Watched, _ = strconv.ParseFloat(f[1], 64)
	}
	if len(f) == 3 {
		s.LastID = f[2]
	}
	return s
}

// watchPoll prices the complete new lines of a transcript (opencode steps
// at worst-case prices, Claude assistant messages at the Claude price file)
// and returns the verdict for the driver: "cost" once the
// running total reaches the ceiling, "timeout" once the deadline passed,
// otherwise "ok". Only whole lines are consumed, so a line the CLI is still
// writing is priced on the next poll.
func watchPoll(format, transcript, statePath string, p *PriceFile, ceiling float64, deadlineMS int64, now time.Time) (string, float64, error) {
	s := readWatchState(statePath)
	if p != nil && transcript != "" {
		f, err := os.Open(transcript)
		if err == nil {
			defer f.Close()
			if _, err := f.Seek(s.Offset, io.SeekStart); err != nil {
				return "", 0, fmt.Errorf("seeking transcript: %w", err)
			}
			r := bufio.NewReaderSize(f, 1<<20)
			for {
				line, err := r.ReadBytes('\n')
				if err != nil {
					break // an incomplete last line waits for the next poll
				}
				s.Offset += int64(len(line))
				if format == "claude" {
					s.Watched += claudeStepCost(line, p, &s.LastID)
				} else {
					s.Watched += stepWorstCost(line, p)
				}
			}
			state := fmt.Sprintf("%d %.6f", s.Offset, s.Watched)
			if s.LastID != "" && !strings.ContainsAny(s.LastID, " \t\r\n") {
				state += " " + s.LastID
			}
			if err := os.WriteFile(statePath, []byte(state+"\n"), 0o644); err != nil {
				return "", 0, fmt.Errorf("writing watch state: %w", err)
			}
		}
	}
	switch {
	case ceiling > 0 && s.Watched >= ceiling:
		return "cost", s.Watched, nil
	case deadlineMS > 0 && now.UnixMilli() >= deadlineMS:
		return "timeout", s.Watched, nil
	}
	return "ok", s.Watched, nil
}

// stepWorstCost prices one opencode step_finish event at the highest
// endpoint prices, cache reads at the highest cache-read price
// (worstCostCached); other lines cost nothing.
func stepWorstCost(line []byte, p *PriceFile) float64 {
	var ev struct {
		Type string `json:"type"`
		Part struct {
			Tokens struct {
				Input     int64 `json:"input"`
				Output    int64 `json:"output"`
				Reasoning int64 `json:"reasoning"`
				Cache     struct {
					Read  int64 `json:"read"`
					Write int64 `json:"write"`
				} `json:"cache"`
			} `json:"tokens"`
		} `json:"part"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Type != "step_finish" || p == nil {
		return 0
	}
	t := ev.Part.Tokens
	return p.worstCostCached(t.Input+t.Cache.Write, t.Cache.Read, t.Output+t.Reasoning)
}
