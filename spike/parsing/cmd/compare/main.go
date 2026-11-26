package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"graphvein/spike/parsing/internal/experiment"
)

type buildResult struct {
	Target      string `json:"target"`
	Success     bool   `json:"success"`
	BinaryBytes int64  `json:"binary_bytes,omitempty"`
	Log         string `json:"log,omitempty"`
}
type optionResult struct {
	Backend     string                  `json:"backend"`
	Builds      []buildResult           `json:"builds"`
	Measurement *experiment.Measurement `json:"measurement,omitempty"`
	RunError    string                  `json:"run_error,omitempty"`
	PeakMemory  string                  `json:"peak_memory"`
}
type report struct {
	GoVersion string         `json:"go_version"`
	Host      string         `json:"host"`
	Compiler  string         `json:"compiler"`
	Options   []optionResult `json:"options"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("comparison failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if err := os.MkdirAll("out", 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile("samples/large.tsx", experiment.LargeSource(), 0644); err != nil {
		return fmt.Errorf("generating workload: %w", err)
	}
	version, err := exec.Command("go", "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("checking Go: %w: %s", err, version)
	}
	compiler, err := exec.Command("gcc", "--version").CombinedOutput()
	compilerInfo := strings.TrimSpace(string(compiler))
	if err != nil {
		compilerInfo = fmt.Sprintf("gcc unavailable: %v", err)
	}
	r := report{GoVersion: strings.TrimSpace(string(version)), Host: runtime.GOOS + "/" + runtime.GOARCH, Compiler: compilerInfo}
	failed := false
	for _, backend := range []string{"wasm", "native"} {
		option := optionResult{Backend: backend, PeakMemory: "not measured (no portable process peak-RSS probe)"}
		for _, target := range []struct{ os, arch string }{{runtime.GOOS, runtime.GOARCH}, {"linux", "amd64"}, {"darwin", "arm64"}} {
			name := backend + "-" + target.os + "-" + target.arch
			if target.os == "windows" {
				name += ".exe"
			}
			path, err := filepath.Abs(filepath.Join("out", name))
			if err != nil {
				return fmt.Errorf("resolving binary path: %w", err)
			}
			// Keep measurements independent of repository ownership and VCS metadata.
			args := []string{"build", "-buildvcs=false", "-o", path}
			cgo := "0"
			if backend == "native" {
				args = append(args, "-tags", "native")
				cgo = "1"
			}
			args = append(args, "./cmd/parse")
			cmd := exec.Command("go", args...)
			cmd.Env = buildEnv(target.os, target.arch, cgo)
			output, buildErr := cmd.CombinedOutput()
			b := buildResult{Target: target.os + "/" + target.arch, Success: buildErr == nil, Log: strings.TrimSpace(string(output))}
			if buildErr != nil && b.Log == "" {
				b.Log = buildErr.Error()
			}
			if buildErr == nil {
				info, err := os.Stat(path)
				if err != nil {
					return fmt.Errorf("measuring binary size: %w", err)
				}
				b.BinaryBytes = info.Size()
			}
			option.Builds = append(option.Builds, b)
			fmt.Printf("%s %s build=%t bytes=%d\n", backend, b.Target, b.Success, b.BinaryBytes)
			if target.os == runtime.GOOS && target.arch == runtime.GOARCH && buildErr == nil {
				cmd := exec.Command(path, "-backend", backend, "-large", "samples/large.tsx")
				var stderr strings.Builder
				cmd.Stderr = &stderr
				data, runErr := cmd.Output()
				if runErr != nil {
					option.RunError = fmt.Sprintf("%v: %s", runErr, stderr.String())
					failed = true
				}
				var m experiment.Measurement
				if err := json.Unmarshal(data, &m); err != nil {
					return fmt.Errorf("decoding %s measurement: %w", backend, err)
				}
				option.Measurement = &m
				if !m.OK() {
					failed = true
				}
				fmt.Printf("%s correct=%t median_ms=%.3f\n", backend, m.OK(), float64(m.MedianNS)/1e6)
			} else if backend == "wasm" && buildErr != nil {
				failed = true
			}
		}
		r.Options = append(r.Options, option)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding comparison: %w", err)
	}
	if err := os.WriteFile("out/results.json", append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing comparison: %w", err)
	}
	if failed {
		return fmt.Errorf("a built backend failed correctness or a WASM build failed; see out/results.json")
	}
	return nil
}

func buildEnv(goos, arch, cgo string) []string {
	env := []string{}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		switch strings.ToUpper(key) {
		case "GOOS", "GOARCH", "CGO_ENABLED":
			continue
		}
		env = append(env, e)
	}
	return append(env, "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED="+cgo)
}
