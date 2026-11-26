package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// allowed is the licence allowlist (ADR 0080): permissive licences whose
// notices THIRD_PARTY_NOTICES reproduces. Anything else, and any licence
// the generator cannot recognise, fails until a reviewed exception or a
// table entry records a human decision.
var allowed = map[string]string{
	"0BSD":                           "",
	"Apache-2.0":                     "",
	"Apache-2.0 WITH LLVM-exception": "",
	"BSD-2-Clause":                   "",
	"BSD-3-Clause":                   "",
	"CC0-1.0":                        "",
	"ISC":                            "",
	"MIT":                            "",
	"MIT-0":                          "",
	"Python-2.0":                     "",
	"Unlicense":                      "",
	"Zlib":                           "",
	"blessing":                       "",
	// Allowed with a note: file-level copyleft. Unmodified files only; a
	// modified file must be published under MPL-2.0.
	"MPL-2.0": "file-level copyleft: ship unmodified, or publish modified files under MPL-2.0",
}

// goModule is the reviewed licence record of one Go module linked into the
// binary. Expression lists every licence its licence files carry.
// Unclassified names licence-like files the classifier cannot read, each
// with the reason it was accepted.
type goModule struct {
	Expression   string
	Unclassified map[string]string
}

// goModules is reviewed by hand: a new module, or a licence file whose
// classification is not in the module's expression, fails the check.
var goModules = map[string]goModule{
	"github.com/dustin/go-humanize":   {Expression: "MIT"},
	"github.com/google/jsonschema-go": {Expression: "MIT"},
	"github.com/google/uuid":          {Expression: "BSD-3-Clause"},
	"github.com/jackc/pgpassfile":     {Expression: "MIT"},
	"github.com/jackc/pgservicefile":  {Expression: "MIT"},
	"github.com/jackc/pgx/v5":         {Expression: "MIT"},
	"github.com/jackc/puddle/v2":      {Expression: "MIT"},
	"github.com/mattn/go-isatty":      {Expression: "MIT"},
	// The MCP SDK (ADR 0082): its LICENSE states the move from MIT to
	// Apache-2.0 (new code Apache-2.0, older code MIT until relicensed,
	// documentation CC-BY-4.0, not compiled into the binary).
	"github.com/modelcontextprotocol/go-sdk":   {Expression: "Apache-2.0 AND MIT"},
	"github.com/ncruces/go-strftime":           {Expression: "MIT"},
	"github.com/remyoudompheng/bigfft":         {Expression: "BSD-3-Clause"},
	"github.com/santhosh-tekuri/jsonschema/v6": {Expression: "Apache-2.0"},
	"github.com/segmentio/asm":                 {Expression: "MIT"},
	"github.com/segmentio/encoding":            {Expression: "MIT"},
	"github.com/tetratelabs/wazero":            {Expression: "Apache-2.0"},
	"github.com/zema1/wasitter":                {Expression: "MIT"},
	"github.com/yosida95/uritemplate/v3":       {Expression: "BSD-3-Clause"},
	"go.yaml.in/yaml/v3":                       {Expression: "MIT AND Apache-2.0"},
	"golang.org/x/oauth2":                      {Expression: "BSD-3-Clause"},
	"golang.org/x/sync":                        {Expression: "BSD-3-Clause"},
	"golang.org/x/sys":                         {Expression: "BSD-3-Clause"},
	"golang.org/x/text":                        {Expression: "BSD-3-Clause"},
	"golang.org/x/time":                        {Expression: "BSD-3-Clause"},
	"modernc.org/libc": {
		Expression: "BSD-3-Clause AND MIT",
		Unclassified: map[string]string{
			"LICENSE-3RD-PARTY.md": "aggregate notice of the Go, musl libc (MIT), go-netdb (MIT) and nixpkgs (MIT) code in the module; reproduced verbatim",
		},
	},
	"modernc.org/mathutil": {Expression: "BSD-3-Clause"},
	"modernc.org/memory": {
		Expression: "BSD-3-Clause",
		Unclassified: map[string]string{
			"LICENSE-LOGO": "a link to the source of the README logo; the logo is not compiled into the binary",
		},
	},
	"modernc.org/sqlite": {
		Expression: "BSD-3-Clause AND blessing AND MIT",
		Unclassified: map[string]string{
			"LICENSE-3RD-PARTY.md": "aggregate notice of the module graph and the transpiled SQLite (public domain) and sqlite-vec (MIT); reproduced verbatim",
		},
	},
}

// npmException is a reviewed licence decision for one exact npm package
// version, recorded in an ADR.
type npmException struct {
	Version, Licence, Chosen, ADR string
}

// npmExceptions mirrors web/scripts/check-licences.mjs: elkjs is dual
// licensed and taken under EPL-2.0 (ADR 0046). A new version fails.
var npmExceptions = map[string]npmException{
	"elkjs": {Version: "0.12.0", Licence: "EPL-2.0 OR GPL-3.0-or-later", Chosen: "EPL-2.0", ADR: "0046"},
}

// goosList are the platforms the binary is built for; the linked module
// set is their union.
var goosList = []string{"linux", "darwin", "windows"}

type linkedModule struct {
	Path, Version, Dir string
}

type npmPackage struct {
	Name, Version, Licence, Kind string
}

type grammarRow struct {
	Asset, Source, Licence, Notice string
}

// errModuleCache reports that the module cache lacks a module some GOOS
// needs; tests skip on it outside CI, as TestDependencySurface does.
type errModuleCache struct{ goos, out string }

func (e errModuleCache) Error() string {
	return fmt.Sprintf("module cache lacks %s dependencies; run go mod download first\n%s", e.goos, e.out)
}

// linkedModules lists the non-main modules linked into cmd/graphvein for
// every GOOS, offline (GOPROXY=off): it never downloads.
func linkedModules(root string) ([]linkedModule, error) {
	seen := map[string]linkedModule{}
	for _, goos := range goosList {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", "./cmd/graphvein")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0", "GOPROXY=off", "GOFLAGS=-mod=mod")
		out, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "module lookup disabled") {
				return nil, errModuleCache{goos, string(out)}
			}
			return nil, fmt.Errorf("go list for %s: %w\n%s", goos, err, out)
		}
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Split(strings.TrimSpace(line), "\t")
			if len(parts) != 3 {
				continue
			}
			seen[parts[0]] = linkedModule{Path: parts[0], Version: parts[1], Dir: parts[2]}
		}
	}
	var mods []linkedModule
	for _, m := range seen {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return mods, nil
}

var licenceFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|patents|authors)([-._].*)?$`)

// licenceFiles returns the licence, notice, patent and author files at the
// root of a module directory, sorted.
func licenceFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && licenceFile.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// fileRole says how a licence-like file is checked: licence files are
// classified, the others are reproduced without classification.
func fileRole(name string) string {
	upper := strings.ToUpper(name)
	switch {
	case strings.HasPrefix(upper, "NOTICE"):
		return "notice"
	case strings.HasPrefix(upper, "PATENTS"):
		return "patents"
	case strings.HasPrefix(upper, "AUTHORS"):
		return "authors"
	}
	return "licence"
}

// classify names the licences a licence text grants, by phrases each
// licence's canonical text contains. It returns nothing for a text it
// cannot read; the caller treats that as a failure unless reviewed.
func classify(text string) []string {
	t := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	var ids []string
	add := func(id string) {
		for _, x := range ids {
			if x == id {
				return
			}
		}
		ids = append(ids, id)
	}
	if strings.Contains(t, "apache license") && strings.Contains(t, "version 2.0") {
		if strings.Contains(t, "llvm exceptions") {
			add("Apache-2.0 WITH LLVM-exception")
		} else {
			add("Apache-2.0")
		}
	}
	if strings.Contains(t, "permission is hereby granted, free of charge") {
		add("MIT")
	}
	if strings.Contains(t, "redistribution and use in source and binary forms") {
		if strings.Contains(t, "neither the name") || strings.Contains(t, "the names of its contributors may not be used") || strings.Contains(t, "the name of the author may not be used") {
			add("BSD-3-Clause")
		} else {
			add("BSD-2-Clause")
		}
	}
	if strings.Contains(t, "permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted") ||
		strings.Contains(t, "permission to use, copy, modify, and distribute this software for any purpose with or without fee is hereby granted") {
		if strings.Contains(t, "provided that the above copyright notice and this permission notice appear in all copies") {
			add("ISC")
		} else {
			add("0BSD")
		}
	}
	if strings.Contains(t, "mozilla public license") && strings.Contains(t, "2.0") {
		add("MPL-2.0")
	}
	if strings.Contains(t, "this is free and unencumbered software released into the public domain") {
		add("Unlicense")
	}
	if strings.Contains(t, "dedicated to the public domain") && strings.Contains(t, "sqlite") {
		add("blessing")
	}
	sort.Strings(ids)
	return ids
}

// expressionIDs splits an SPDX expression of AND/OR terms into licence
// identifiers ("X WITH Y" stays one identifier).
func expressionIDs(expr string) []string {
	var ids []string
	for _, part := range regexp.MustCompile(` (AND|OR) `).Split(expr, -1) {
		ids = append(ids, strings.Trim(strings.TrimSpace(part), "()"))
	}
	return ids
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// npmPackages reads the runtime packages of web/package-lock.json (not dev,
// not devOptional) and the build tools whose code the build copies into the
// bundle (BUNDLED_TOOLS in web/scripts/bundled-tools.mjs).
func npmPackages(root string) ([]npmPackage, error) {
	data, err := os.ReadFile(filepath.Join(root, "web", "package-lock.json"))
	if err != nil {
		return nil, fmt.Errorf("reading web/package-lock.json: %w", err)
	}
	var lock struct {
		Packages map[string]struct {
			Version     string `json:"version"`
			License     string `json:"license"`
			Dev         bool   `json:"dev"`
			DevOptional bool   `json:"devOptional"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing web/package-lock.json: %w", err)
	}
	tools, err := bundledTools(root)
	if err != nil {
		return nil, err
	}
	var out []npmPackage
	for key, p := range lock.Packages {
		if key == "" {
			continue
		}
		name := key[strings.LastIndex(key, "node_modules/")+len("node_modules/"):]
		switch {
		case !p.Dev && !p.DevOptional:
			out = append(out, npmPackage{Name: name, Version: p.Version, Licence: p.License, Kind: "runtime"})
		case key == "node_modules/"+name && contains(tools, name):
			out = append(out, npmPackage{Name: name, Version: p.Version, Licence: p.License, Kind: "bundled build tool"})
		}
	}
	for _, tool := range tools {
		if _, ok := lock.Packages["node_modules/"+tool]; !ok {
			return nil, fmt.Errorf("bundled tool %s is not in web/package-lock.json", tool)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

var bundledToolsLine = regexp.MustCompile(`(?m)^export const BUNDLED_TOOLS = \[([^\]]*)\];`)

func bundledTools(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "web", "scripts", "bundled-tools.mjs"))
	if err != nil {
		return nil, fmt.Errorf("reading web/scripts/bundled-tools.mjs: %w", err)
	}
	m := bundledToolsLine.FindSubmatch(data)
	if m == nil {
		return nil, fmt.Errorf("web/scripts/bundled-tools.mjs: no BUNDLED_TOOLS list")
	}
	var tools []string
	for _, part := range strings.Split(string(m[1]), ",") {
		if s := strings.Trim(strings.TrimSpace(part), `"'`); s != "" {
			tools = append(tools, s)
		}
	}
	return tools, nil
}

// grammarRows reads the asset table of grammars/NOTICES.md.
func grammarRows(root string) ([]grammarRow, error) {
	data, err := os.ReadFile(filepath.Join(root, "grammars", "NOTICES.md"))
	if err != nil {
		return nil, fmt.Errorf("reading grammars/NOTICES.md: %w", err)
	}
	var rows []grammarRow
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "| Asset |") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		if strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 4 {
			return nil, fmt.Errorf("grammars/NOTICES.md: row has %d cells: %s", len(cells), line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, grammarRow{Asset: cells[0], Source: cells[1], Licence: cells[2], Notice: cells[3]})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("grammars/NOTICES.md has no asset table")
	}
	return rows, nil
}

// grammarLicences reads a grammar row's licence cell: "MIT", or a recorded
// discrepancy such as "MIT text; ISC metadata" (both terms must be allowed).
func grammarLicences(cell string) []string {
	var ids []string
	for _, part := range strings.Split(cell, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			ids = append(ids, fields[0])
		}
	}
	return ids
}

// grammarNoticeFiles are the files of grammars/ reproduced verbatim: every
// grammar's licence, the upstream release notices and the toolchain notices.
func grammarNoticeFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "grammars"))
	if err != nil {
		return nil, fmt.Errorf("reading grammars: %w", err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, "-LICENSE.txt") || n == "UPSTREAM_NOTICES.txt" || n == "WASI-SDK-NOTICES.txt" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

func normalise(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimRight(text, " \t\n") + "\n"
}

// Generate builds THIRD_PARTY_NOTICES for the repository at root and
// returns it with every licence policy problem found. A non-empty problem
// list means the licence check fails.
func Generate(root string) (string, []string, error) {
	var problems []string
	mods, err := linkedModules(root)
	if err != nil {
		return "", nil, err
	}
	npm, err := npmPackages(root)
	if err != nil {
		return "", nil, err
	}
	grammars, err := grammarRows(root)
	if err != nil {
		return "", nil, err
	}
	grammarFiles, err := grammarNoticeFiles(root)
	if err != nil {
		return "", nil, err
	}

	rule := strings.Repeat("-", 72)
	var b strings.Builder
	b.WriteString("Third-party notices for Graphvein\n")
	b.WriteString("=================================\n\n")
	b.WriteString("Graphvein is licensed under the Apache License 2.0 (LICENSE, NOTICE).\n")
	b.WriteString("The graphvein binary includes the third-party software listed below.\n")
	b.WriteString("Generated by `go run ./scripts/notices` from the Go module graph of\n")
	b.WriteString("cmd/graphvein (linux, darwin, windows), grammars/ and\n")
	b.WriteString("web/package-lock.json. Do not edit by hand.\n\n")

	b.WriteString("1. Go modules linked into the binary\n\n")
	used := map[string]bool{}
	var appendix strings.Builder
	for _, m := range mods {
		used[m.Path] = true
		rec, ok := goModules[m.Path]
		if !ok {
			problems = append(problems, fmt.Sprintf("Go module %s %s has no reviewed licence record in scripts/notices", m.Path, m.Version))
			rec = goModule{Expression: "UNREVIEWED"}
		}
		for _, id := range expressionIDs(rec.Expression) {
			if _, ok := allowed[id]; !ok && rec.Expression != "UNREVIEWED" {
				problems = append(problems, fmt.Sprintf("Go module %s: licence %s is not on the allowlist", m.Path, id))
			}
		}
		fmt.Fprintf(&b, "  %s %s: %s\n", m.Path, m.Version, rec.Expression)
		files, err := licenceFiles(m.Dir)
		if err != nil {
			return "", nil, err
		}
		classified := false
		fmt.Fprintf(&appendix, "\n%s\n%s %s\nLicence: %s\n", rule, m.Path, m.Version, rec.Expression)
		for _, name := range files {
			data, err := os.ReadFile(filepath.Join(m.Dir, name))
			if err != nil {
				return "", nil, fmt.Errorf("reading %s of %s: %w", name, m.Path, err)
			}
			if fileRole(name) == "licence" {
				if reason, ok := rec.Unclassified[name]; ok {
					fmt.Fprintf(&appendix, "(%s: %s)\n", name, reason)
				} else if ids := classify(string(data)); len(ids) == 0 {
					problems = append(problems, fmt.Sprintf("Go module %s: licence file %s is not recognised", m.Path, name))
				} else {
					classified = true
					for _, id := range ids {
						if !contains(expressionIDs(rec.Expression), id) {
							problems = append(problems, fmt.Sprintf("Go module %s: %s reads as %s, which the reviewed expression %q does not name", m.Path, name, id, rec.Expression))
						}
					}
				}
			}
			fmt.Fprintf(&appendix, "\n--- %s ---\n\n%s", name, normalise(string(data)))
		}
		if !classified {
			problems = append(problems, fmt.Sprintf("Go module %s: no recognised licence file", m.Path))
		}
	}
	for path := range goModules {
		if !used[path] {
			problems = append(problems, fmt.Sprintf("scripts/notices reviews Go module %s, which the binary no longer links; remove the entry", path))
		}
	}

	b.WriteString("\n2. Tree-sitter grammars (WebAssembly modules in internal/parse/grammars)\n\n")
	b.WriteString("Built from the pinned sources in grammars/manifest.yaml; provenance and\n")
	b.WriteString("discrepancies are recorded in grammars/NOTICES.md. The notices are\n")
	b.WriteString("reproduced in Appendix B.\n\n")
	for _, g := range grammars {
		for _, id := range grammarLicences(g.Licence) {
			if _, ok := allowed[id]; !ok {
				problems = append(problems, fmt.Sprintf("grammar %s: licence %q is not on the allowlist", g.Asset, g.Licence))
			}
		}
		fmt.Fprintf(&b, "  %s: %s; %s\n", g.Asset, g.Source, g.Licence)
	}

	b.WriteString("\n3. Web UI (embedded in the binary)\n\n")
	b.WriteString("Runtime packages of web/package-lock.json and the build tools whose\n")
	b.WriteString("code the build copies into the bundle. Their full licence texts, the\n")
	b.WriteString("shadcn/ui components, the Go standard library notice and the editor\n")
	b.WriteString("logo attributions are in internal/server/ui/static/third-party-licenses.txt,\n")
	b.WriteString("which the binary serves at /third-party-licenses.txt. The editor logos\n")
	b.WriteString("are not covered by the Apache License (see NOTICE).\n\n")
	usedExceptions := map[string]bool{}
	for _, p := range npm {
		note := ""
		if x, ok := npmExceptions[p.Name]; ok {
			usedExceptions[p.Name] = true
			if p.Version != x.Version || p.Licence != x.Licence || p.Kind != "runtime" {
				problems = append(problems, fmt.Sprintf("npm package %s %s (%s, %s) does not match its ADR %s exception (%s %s); review the new version", p.Name, p.Version, p.Kind, p.Licence, x.ADR, x.Version, x.Licence))
			}
			note = fmt.Sprintf(" (taken under %s, ADR %s)", x.Chosen, x.ADR)
		} else if _, ok := allowed[p.Licence]; !ok {
			problems = append(problems, fmt.Sprintf("npm package %s %s: licence %q is not on the allowlist", p.Name, p.Version, p.Licence))
		}
		fmt.Fprintf(&b, "  %s %s: %s%s, %s\n", p.Name, p.Version, p.Licence, note, p.Kind)
	}
	for name := range npmExceptions {
		if !usedExceptions[name] {
			problems = append(problems, fmt.Sprintf("npm exception for %s is not used; remove it", name))
		}
	}

	b.WriteString("\nAppendix A. Go module licence and notice files (verbatim)\n")
	b.WriteString(appendix.String())

	b.WriteString("\nAppendix B. Grammar and WebAssembly runtime notices (verbatim, from grammars/)\n")
	for _, name := range grammarFiles {
		data, err := os.ReadFile(filepath.Join(root, "grammars", name))
		if err != nil {
			return "", nil, fmt.Errorf("reading grammars/%s: %w", name, err)
		}
		fmt.Fprintf(&b, "\n%s\ngrammars/%s\n\n%s", rule, name, normalise(string(data)))
	}
	sort.Strings(problems)
	return b.String(), problems, nil
}
