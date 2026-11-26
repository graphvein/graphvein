package server

import (
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/index"
)

// File categories of the couldn't-read page (rows CR-36, CR-37 of
// docs/contract/claims/couldnt-read.md). A category describes a file's
// name only, so the UI can show code without a reader before documents and
// images; it claims nothing about the content.
const (
	// categoryReader is source of a language a built-in reader parses
	// (index.ReadsPath: .py, .ts, .go). Only a file not read can have it:
	// a file of a reader's language is never a file without facts (row
	// CR-23).
	categoryReader = "code_with_reader"
	// categoryCode is source of a programming, style sheet, query or
	// component language that no built-in reader parses (.vue, .rb, .sql).
	categoryCode = "code_no_reader"
	// categoryNotCode is documentation, images, fonts, media, archives,
	// data and configuration (.md, .svg, .png, .json).
	categoryNotCode = "not_code"
	// categoryOther is every other extension, and a file without one.
	categoryOther = "other"
)

// fileCategoryNames are the categories in the order the page counts them.
var fileCategoryNames = []string{categoryReader, categoryCode, categoryNotCode, categoryOther}

// fileCategories is the extension table of row CR-36, by lower-cased final
// extension. TestUnreadCategories keeps it equal to the table in the claim
// file; an extension it does not hold is categoryOther.
var fileCategories = map[string]string{
	// code_no_reader
	".astro":   categoryCode,
	".bash":    categoryCode,
	".bat":     categoryCode,
	".c":       categoryCode,
	".cc":      categoryCode,
	".clj":     categoryCode,
	".cljs":    categoryCode,
	".cmd":     categoryCode,
	".coffee":  categoryCode,
	".cpp":     categoryCode,
	".css":     categoryCode,
	".cxx":     categoryCode,
	".elm":     categoryCode,
	".erl":     categoryCode,
	".ex":      categoryCode,
	".exs":     categoryCode,
	".fish":    categoryCode,
	".fs":      categoryCode,
	".gql":     categoryCode,
	".gradle":  categoryCode,
	".graphql": categoryCode,
	".groovy":  categoryCode,
	".h":       categoryCode,
	".hh":      categoryCode,
	".hpp":     categoryCode,
	".hs":      categoryCode,
	".jl":      categoryCode,
	".kt":      categoryCode,
	".kts":     categoryCode,
	".less":    categoryCode,
	".lua":     categoryCode,
	".m":       categoryCode,
	".ml":      categoryCode,
	".mm":      categoryCode,
	".nim":     categoryCode,
	".php":     categoryCode,
	".pl":      categoryCode,
	".pm":      categoryCode,
	".proto":   categoryCode,
	".ps1":     categoryCode,
	".r":       categoryCode,
	".rb":      categoryCode,
	".sass":    categoryCode,
	".scala":   categoryCode,
	".scss":    categoryCode,
	".sh":      categoryCode,
	".sol":     categoryCode,
	".sql":     categoryCode,
	".svelte":  categoryCode,
	".swift":   categoryCode,
	".tf":      categoryCode,
	".vb":      categoryCode,
	".vue":     categoryCode,
	".zig":     categoryCode,
	".zsh":     categoryCode,
	// not_code
	".adoc":       categoryNotCode,
	".avif":       categoryNotCode,
	".bmp":        categoryNotCode,
	".cfg":        categoryNotCode,
	".conf":       categoryNotCode,
	".csv":        categoryNotCode,
	".env":        categoryNotCode,
	".eot":        categoryNotCode,
	".gif":        categoryNotCode,
	".gz":         categoryNotCode,
	".ico":        categoryNotCode,
	".ini":        categoryNotCode,
	".jpeg":       categoryNotCode,
	".jpg":        categoryNotCode,
	".json":       categoryNotCode,
	".json5":      categoryNotCode,
	".jsonc":      categoryNotCode,
	".lock":       categoryNotCode,
	".log":        categoryNotCode,
	".markdown":   categoryNotCode,
	".md":         categoryNotCode,
	".mdx":        categoryNotCode,
	".mov":        categoryNotCode,
	".mp3":        categoryNotCode,
	".mp4":        categoryNotCode,
	".ogg":        categoryNotCode,
	".otf":        categoryNotCode,
	".pdf":        categoryNotCode,
	".png":        categoryNotCode,
	".properties": categoryNotCode,
	".psd":        categoryNotCode,
	".rst":        categoryNotCode,
	".svg":        categoryNotCode,
	".tar":        categoryNotCode,
	".tgz":        categoryNotCode,
	".tif":        categoryNotCode,
	".tiff":       categoryNotCode,
	".toml":       categoryNotCode,
	".tsv":        categoryNotCode,
	".ttf":        categoryNotCode,
	".txt":        categoryNotCode,
	".wav":        categoryNotCode,
	".webm":       categoryNotCode,
	".webp":       categoryNotCode,
	".woff":       categoryNotCode,
	".woff2":      categoryNotCode,
	".xml":        categoryNotCode,
	".yaml":       categoryNotCode,
	".yml":        categoryNotCode,
	".zip":        categoryNotCode,
}

// fileCategory is the category of a file path (row CR-36): a reader's
// extension first, then the table.
func fileCategory(file string) string {
	if index.ReadsPath(file) {
		return categoryReader
	}
	if c, ok := fileCategories[strings.ToLower(path.Ext(file))]; ok {
		return c
	}
	return categoryOther
}

// categoryCount is the count of one category (row CR-37).
type categoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// countCategories counts files per category, every category present.
func countCategories(files []string) []categoryCount {
	n := map[string]int{}
	for _, f := range files {
		n[fileCategory(f)]++
	}
	out := make([]categoryCount, 0, len(fileCategoryNames))
	for _, c := range fileCategoryNames {
		out = append(out, categoryCount{Category: c, Count: n[c]})
	}
	return out
}
