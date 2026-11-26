//go:build graphveincorpus

package index_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

func verifyRouteCorpus(t *testing.T, name, pin string) string {
	t.Helper()
	root := filepath.Join("../../spike/parsing/.corpus", name)
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		data, err := command.Output()
		if err != nil {
			t.Fatalf("verifying %s: %v", name, err)
		}
		return strings.TrimSpace(string(data))
	}
	if got := git("rev-parse", "HEAD"); got != pin {
		t.Fatalf("%s pin mismatch: %s, expected %s", name, got, pin)
	}
	if status := git("status", "--porcelain"); status != "" {
		t.Fatalf("%s is dirty: %s", name, status)
	}
	return root
}

func summarizeRouteCorpus(t *testing.T, root, name string) store.Snapshot {
	t.Helper()
	s := open(t)
	_, summary, _ := emit(t, root, s, options(t))
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	exposed, routes, unresolved := 0, 0, 0
	for _, d := range snapshot.Diagnostics {
		counts[d.Code]++
		if d.Code == "extract_invalid_record" || d.Code == "extract_failed" || d.Code == "parse_failed" {
			t.Error(name, d)
		}
		if d.Code == "route_mount_unresolved" {
			t.Logf("%s mount unresolved: %s: %s", name, d.File, d.Message)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "exposes_route" {
			exposed++
		}
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			routes++
			if _, ok := n.Attributes["handler_expression"]; ok {
				unresolved++
			}
			if strings.HasSuffix(n.QualifiedName, "@2") {
				t.Log("repeated registration:", n.Identity)
			}
		}
	}
	data, _ := json.Marshal(counts)
	t.Logf("%s: files=%d parsed=%d routes=%d exposes_route=%d unresolved_handlers=%d diagnostics=%s", name, summary.Files, summary.Parsed, routes, exposed, unresolved, data)
	return snapshot
}

func TestExpressCorpus(t *testing.T) {
	root := verifyRouteCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	snapshot := summarizeRouteCorpus(t, root, "express")
	parameter := regexp.MustCompile(`\{[^}]+\}`)
	canonical := func(s string) string { return parameter.ReplaceAllString(s, "{}") }
	operations := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			for _, p := range routePaths(t, n) {
				operations[canonical(n.Attributes["method"].(string)+" "+p["template"].(string))] = true
			}
		}
	}
	file, err := os.Open("../../testdata/corpus/realworld-api.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	required := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		required[canonical(line)] = true
		if !operations[canonical(line)] {
			t.Error("missing operation", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(required) != 19 {
		t.Fatal("ground truth count", len(required))
	}
	extras := []string{}
	for operation := range operations {
		if !required[operation] {
			extras = append(extras, operation)
		}
	}
	sort.Strings(extras)
	t.Log("extra operations:", extras)
	if strings.Join(extras, "|") != "GET /|GET /api/health-check|GET /swagger.json" {
		t.Error("unexpected extra operations", extras)
	}
}

func TestFastAPICorpus(t *testing.T) {
	root := verifyRouteCorpus(t, "fastapi", "1762adac607a1b29cfc4da129557780beea71616")
	snapshot := summarizeRouteCorpus(t, root, "fastapi")
	parameter := regexp.MustCompile(`\{[^}]+\}`)
	canonical := func(method, pattern string) string {
		parts := strings.FieldsFunc(pattern, func(r rune) bool { return r == '/' })
		return strings.ToUpper(method) + " " + parameter.ReplaceAllString("/"+strings.Join(parts, "/"), "{}")
	}
	operations := map[string]bool{}
	exposed := map[string]bool{}
	for _, e := range snapshot.Edges {
		if e.Kind == "exposes_route" {
			exposed[e.Target] = true
		}
	}
	count := 0
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		count++
		if !exposed[n.Identity] {
			t.Error("route missing handler edge", n.Identity)
		}
		for _, p := range routePaths(t, n) {
			operations[canonical(n.Attributes["method"].(string), p["template"].(string))] = true
			if p["resolution"] != "settings_default" {
				t.Error("API_V1_STR path resolution", n.Identity, p)
			}
			if got, want := p["conditional"], n.File == "backend/app/api/routes/private.py"; got != want {
				t.Error("conditional include", n.Identity, p)
			}
		}
	}
	if count != 23 {
		t.Error("route count", count)
	}
	file, err := os.Open("../../testdata/corpus/fastapi-template-api.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	required := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		method, pattern, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatal("invalid operation", line)
		}
		key := canonical(method, pattern)
		required[key] = true
		if !operations[key] {
			t.Error("missing SDK operation", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(required) != 23 {
		t.Fatal("SDK ground truth count", len(required))
	}
	for operation := range operations {
		if !required[operation] {
			t.Error("unexpected extra operation", operation)
		}
	}
}

// Run this sweep separately from the normal suite and ExpressCorpus. Subtests
// are intentionally sequential; no corpus indexing overlaps another test run.
func TestRouteCorpusSweep(t *testing.T) {
	for _, entry := range []struct{ name, pin string }{
		{"react", "969709a379b13935b4e1caae0ad8cad548e5879a"},
		{"angular", "dd99ed2cf39c805d719f943c5d7061a5683d98a8"},
		{"fastapi", "1762adac607a1b29cfc4da129557780beea71616"},
		{"axum", "f1b25654773228297e35c292f357d33b7121a101"},
		{"axum-ddd", "a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2"},
		{"langflow", "504c02fc47e76087b82b0e7cbe4186e9cdd916d4"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			root := verifyRouteCorpus(t, entry.name, entry.pin)
			snapshot := summarizeRouteCorpus(t, root, entry.name)
			for _, n := range snapshot.Nodes {
				if n.Kind == "route" && entry.name != "fastapi" && entry.name != "langflow" && entry.name != "axum" && entry.name != "axum-ddd" {
					t.Error("unexpected Express route", n.Identity)
				}
			}
		})
	}
	t.Run("self", func(t *testing.T) { summarizeRouteCorpus(t, "../..", "graphvein") })
}

func TestAxumCorpus(t *testing.T) {
	root := verifyRouteCorpus(t, "axum", "f1b25654773228297e35c292f357d33b7121a101")
	snapshot := summarizeRouteCorpus(t, root, "axum")
	parameter := regexp.MustCompile(`\{[^}]+\}`)
	canonical := func(s string) string { return parameter.ReplaceAllString(strings.TrimRight(s, "/"), "{}") }
	operations := map[string]bool{}
	exposed := map[string]bool{}
	for _, e := range snapshot.Edges {
		if e.Kind == "exposes_route" {
			exposed[e.Target] = true
		}
	}
	count := 0
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		count++
		if !exposed[n.Identity] {
			t.Error("missing handler edge", n.Identity)
		}
		for _, p := range routePaths(t, n) {
			operations[canonical(n.Attributes["method"].(string)+" "+p["template"].(string))] = true
		}
	}
	data, err := os.ReadFile("../../testdata/corpus/realworld-api.txt")
	if err != nil {
		t.Fatal(err)
	}
	required := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		required++
		if !operations[canonical(line)] {
			t.Error("missing operation", line)
		}
	}
	if required != 19 || count != 19 || len(operations) != 19 {
		t.Fatal("route/operation count", count, len(operations), required)
	}
}
