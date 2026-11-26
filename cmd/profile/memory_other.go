//go:build graphveinprofile && !windows

package main

func processMemory() (uint64, uint64, error) { return 0, 0, nil }

func hostEnvironment() map[string]any {
	return map[string]any{"cpu_model": "not measured: Windows measurement harness", "ram_bytes": "not measured: Windows measurement harness"}
}
