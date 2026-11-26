// Command coverage verifies T002 goldens or probes an upstream WASM artifact.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/zema1/wasitter"
	"graphvein/spike/parsing/internal/coverage"
	"os"
)

func main() {
	probe := flag.String("probe", "", "upstream prebuilt WASM to test for adapter compatibility")
	flag.Parse()
	if *probe != "" {
		data, err := os.ReadFile(*probe)
		if err == nil {
			var p *wasitter.Parser
			var r *wasitter.Runtime
			p, r, err = wasitter.NewParserFromWASM(context.Background(), data)
			if err == nil {
				_ = p.Close()
				_ = r.Close()
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("compatible")
		return
	}
	results := coverage.Run()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, result := range results {
		if result.Error != "" {
			os.Exit(1)
		}
	}
}
