package typescript

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

func TestInjectionCaptureLexicalField(t *testing.T) {
	source := []byte(`import { Injectable, inject } from '@angular/core';
class Local {}
export function enclosing() {
  class Local {}
  @Injectable()
  class FieldScope { x = inject(Local); }
  return FieldScope;
}`)
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	root := results[0].Root
	f := Extract("lexical.ts", source, root)
	s := CaptureInjections(root, f)
	if !s.Valid() || len(s.Sites) != 1 || len(s.Sites[0].Shadows) != 1 || s.Sites[0].Shadows[0] != "Local" {
		t.Fatalf("field lexical syntax: %+v", s)
	}
}

func TestInjectionCapturePayload(t *testing.T) {
	source := []byte(`import { inject as use, ordinary } from './barrel';
import { acquire } from './renamed';
import * as core from '@angular/core';
class Local {}
export function named() { use(Local, { optional: ordinary() }); }
export function namespace() { core.inject(Local, { optional: true }); }
export function unrelated(value = ordinary()) { ordinary(Local); acquire(Local); }
`)
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	root := results[0].Root
	s := CaptureInjections(root, Extract("payload.ts", source, root))
	if !s.Valid() || len(s.Sites) != 2 || len(s.ParameterScopes) != 2 {
		t.Fatalf("unexpected payload: %+v", s)
	}
	for _, site := range s.Sites {
		if len(site.Call.Args) != 1 || site.Call.Args[0].Text != "Local" || site.Call.Text != "" || site.Call.Value.Kind != "" {
			t.Error("site retained options or duplicate call syntax", site)
		}
	}
	s.Version = 1
	if s.Valid() {
		t.Error("checkpoint payload accepted without revision syntax")
	}
	s.Version = 2
	s.Sites[0].Shadows = nil
	if s.Valid() {
		t.Error("truncated shadow proof accepted")
	}
}
