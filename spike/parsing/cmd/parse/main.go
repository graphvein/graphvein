package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"os"

	"graphvein/spike/parsing/internal/experiment"
)

func main() {
	backend := flag.String("backend", "wasm", "wasm or native")
	large := flag.String("large", "", "optional large TSX input for five parse timings")
	flag.Parse()
	result := experiment.Run(*backend, *large)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		slog.Error("writing results", "error", err)
		os.Exit(1)
	}
	if !result.OK() {
		os.Exit(1)
	}
}
