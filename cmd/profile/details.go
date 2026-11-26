//go:build graphveinprofile

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

type detailStats struct {
	ReplayValidationNS     int64
	ReplayAllocatedBytes   uint64
	ReplayAllocatedObjects uint64
	ReplayFiles            int
	IngestNS               map[string]int64
	Maps                   []sqlstore.ProfileMapSnapshot
}

func enableDetails() *detailStats {
	d := &detailStats{IngestNS: map[string]int64{}, Maps: []sqlstore.ProfileMapSnapshot{}}
	index.ProfileObserver = func(elapsed time.Duration, allocated, objects uint64) {
		d.ReplayValidationNS += int64(elapsed)
		d.ReplayAllocatedBytes += allocated
		d.ReplayAllocatedObjects += objects
		d.ReplayFiles++
	}
	ingest.ProfileObserver = func(elapsed time.Duration) { d.IngestNS["wire_read_validate"] += int64(elapsed) }
	sqlstore.ProfileObserver = func(phase string, elapsed time.Duration) { d.IngestNS[phase] += int64(elapsed) }
	sqlstore.ProfileMapObserver = func(snapshot sqlstore.ProfileMapSnapshot) { d.Maps = append(d.Maps, snapshot) }
	return d
}

// This mode appends the instrumented samples after a baseline run, without
// repeating its measurements. The regular driver also runs these scenarios.
func runDetails(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("details requires external output directory and sample count")
	}
	out, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if strings.EqualFold(root, out) || strings.HasPrefix(strings.ToLower(out), strings.ToLower(root)+string(filepath.Separator)) {
		return fmt.Errorf("profile output must be outside repository")
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 1 {
		return fmt.Errorf("positive sample count required")
	}
	corpus := "spike/parsing/.corpus/langflow"
	head, stderr, err := command("git", []string{"-C", corpus, "rev-parse", "HEAD"}, "")
	if err != nil {
		return fmt.Errorf("verifying pin: %w %s", err, stderr)
	}
	status, stderr, err := command("git", []string{"-C", corpus, "status", "--porcelain"}, "")
	if err != nil {
		return fmt.Errorf("verifying status: %w %s", err, stderr)
	}
	if strings.TrimSpace(string(head)) != "504c02fc47e76087b82b0e7cbe4186e9cdd916d4" || len(status) != 0 {
		return fmt.Errorf("Langflow pin or clean status mismatch; stopping")
	}
	work, err := os.MkdirTemp("", "graphvein-profile-details-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	samples := []sample{}
	for i := 0; i < n; i++ {
		db := filepath.Join(work, fmt.Sprintf("detail-%d.db", i))
		for _, phase := range []string{"detail-cold", "detail-warm", "detail-warm-w1"} {
			id := fmt.Sprintf("%s-%d", phase, i)
			snap := filepath.Join(work, id+".ndjson")
			workers := "4"
			if phase == "detail-warm-w1" {
				workers = "1"
			}
			b, stderr, err := command(os.Args[0], []string{"child", "langflow", corpus, db, phase, workers, snap, ""}, "")
			if err != nil {
				return fmt.Errorf("%s: %w %s", id, err, stderr)
			}
			var s sample
			if err = json.Unmarshal(b, &s); err != nil {
				return err
			}
			if strings.Contains(string(stderr), "using memory") {
				s.CompilationCache = "memory fallback (sandbox cache denied)"
			} else if strings.Contains(string(stderr), "rebuilding WASM") {
				s.CompilationCache = "cache rebuild warning; disk versus memory not established"
			} else if s.Summary.Parsed > 0 {
				s.CompilationCache = "default disk cache; no fallback warning"
			}
			if err = save(filepath.Join(out, id+".json"), s); err != nil {
				return err
			}
			if err = os.WriteFile(filepath.Join(out, id+".stderr.txt"), stderr, 0644); err != nil {
				return err
			}
			samples = append(samples, s)
			fmt.Fprintf(os.Stdout, "Q4/Q5 %s: index=%.1fms ingest=%.1fms replay-validation=%.1fms allocation=%.1fMiB phases=%v maps=%+v\n", id, float64(s.Summary.Total)/1e6, s.IngestMS, float64(s.Details.ReplayValidationNS)/1e6, float64(s.Details.ReplayAllocatedBytes)/(1<<20), s.Details.IngestNS, s.Details.Maps)
		}
	}
	return save(filepath.Join(out, "details.json"), samples)
}

func aggregateDetails(samples []sample) map[string]map[string][]float64 {
	grouped := map[string]map[string][]float64{}
	for _, s := range samples {
		if !s.Instrumented || s.Details == nil {
			continue
		}
		key := s.Run
		if grouped[key] == nil {
			grouped[key] = map[string][]float64{}
		}
		values := map[string]float64{"index_ms": float64(s.Summary.Total) / 1e6, "ingest_ms": s.IngestMS, "replay_validation_ms": float64(s.Details.ReplayValidationNS) / 1e6, "replay_allocated_mib": float64(s.Details.ReplayAllocatedBytes) / (1 << 20), "replay_allocated_objects": float64(s.Details.ReplayAllocatedObjects), "replay_share_percent": float64(s.Details.ReplayValidationNS) * 100 / float64(s.Summary.Total)}
		observed := int64(0)
		for phase, ns := range s.Details.IngestNS {
			values[phase+"_ms"] = float64(ns) / 1e6
			observed += ns
		}
		values["other_ingest_ms"] = s.IngestMS - float64(observed)/1e6
		for _, m := range s.Details.Maps {
			values[m.Stage+"_payload_mib"] = float64(m.OldPayloadBytes) / (1 << 20)
			values[m.Stage+"_referenced_string_mib"] = float64(m.UniqueStringBytes) / (1 << 20)
		}
		for field, v := range values {
			grouped[key][field] = append(grouped[key][field], v)
		}
	}
	for _, fields := range grouped {
		for field, v := range fields {
			sort.Float64s(v)
			median := v[len(v)/2]
			if len(v)%2 == 0 {
				median = (median + v[len(v)/2-1]) / 2
			}
			fields[field] = []float64{median, v[0], v[len(v)-1]}
		}
	}
	return grouped
}
