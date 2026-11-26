package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func TestStaticCallsGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/static-calls")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/static-calls.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	targets := map[string]int{}
	linkedWrapper := false
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" {
			targets[e.Target] += len(e.Evidence)
		}
		if e.Kind == "calls_route" && e.Source == "client_call:app.ts#invoke.GET%20%27%2Ft012f%2Fwrapped%27" && e.Target == "route:app.ts#GET%20%2Ft012f%2Fwrapped" {
			linkedWrapper = true
		}
	}
	for target, count := range map[string]int{
		"method:app.ts#Local.run":                1,
		"method:anonymous.ts#default.class.run":  1,
		"method:service.ts#Service.plain":        5,
		"method:service.ts#Service.%27quoted%27": 1,
		"method:service.ts#DefaultService.run":   1,
		"method:service.ts#Service.overloaded":   1,
		"method:service.ts#Service.asynchronous": 1,
		"method:service.ts#Service.generator":    1,
		"method:service.ts#Service.send":         1,
		"method:rejections.ts#Own.direct":        1,
		"method:rejections.ts#Replay.run":        1,
		"method:rejections.ts#Assigned.safe":     1,
	} {
		if targets[target] != count {
			t.Errorf("%s: got %d sites, want %d", target, targets[target], count)
		}
		delete(targets, target)
	}
	if len(targets) != 0 || !linkedWrapper {
		t.Fatal("unexpected static edges or missing wrapper", targets, linkedWrapper)
	}
	warmData, warm, writes := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("static replay differs", warm, writes)
	}
	// A mutation-only edit withdraws an unchanged imported caller's proof.
	mutationFile := filepath.Join(root, "mutations.ts")
	f, err := os.OpenFile(mutationFile, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\nimport { Replay } from './rejections';\nReplay.run = () => 3;\n")
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 {
		t.Fatal("mutation should reparse only its file", changed)
	}
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" && e.Target == "method:rejections.ts#Replay.run" {
			t.Fatal("cached caller retained mutated target", e)
		}
	}
}
