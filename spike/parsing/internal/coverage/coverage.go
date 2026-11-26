// Package coverage checks T002 grammar loading and small, hand-reviewed trees.
package coverage

import (
	"fmt"
	"graphvein/spike/parsing/grammars"
	"graphvein/spike/parsing/internal/experiment"
	"graphvein/spike/parsing/internal/parse"
	"os"
)

// SamplePath shares the same source between golden checks and cold-start runs.
func SamplePath(language string) string {
	switch language {
	case "tsx":
		return "samples/items.tsx"
	case "rust":
		return "samples/items.rs"
	case "javascript":
		return "samples/javascript.jsx"
	case "typescript":
		return "samples/typescript.ts"
	case "go":
		return "samples/go.go"
	case "python":
		return "samples/python.py"
	case "java":
		return "samples/java.java"
	case "html", "angular":
		return "samples/" + language + ".html"
	case "c_sharp":
		return "samples/c_sharp.cs"
	case "dart":
		return "samples/dart.dart"
	default:
		return ""
	}
}

// Result includes the guest's actual language ABI rather than guessing from tags.
type Result struct {
	Language string        `json:"language"`
	ABI      uint32        `json:"abi"`
	Entries  []parse.Entry `json:"entries"`
	Error    string        `json:"error,omitempty"`
}

// Check validates one pinned grammar without preventing subsequent checks.
func Check(language string) (result Result) {
	result.Language = language
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Error = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	source, err := os.ReadFile(SamplePath(language))
	if err != nil {
		result.Error = err.Error()
		return
	}
	p, err := parse.New("wasm", language)
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer p.Close()
	result.ABI = p.(interface{ ABIVersion() uint32 }).ABIVersion()
	if result.ABI < 13 || result.ABI > 15 {
		result.Error = fmt.Sprintf("unsupported ABI %d", result.ABI)
		return
	}
	tree, err := p.Parse(source)
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer tree.Close()
	result.Entries, err = parse.Extract(tree, source)
	if err == nil {
		err = experiment.CheckGolden(result.Entries, SamplePath(language)+".golden.json")
	}
	if err != nil {
		result.Error = err.Error()
	}
	return
}

// Run keeps coverage in release order, making JSON and test diffs readable.
func Run() []Result {
	results := []Result{}
	for _, language := range grammars.Available() {
		results = append(results, Check(language))
	}
	return results
}
