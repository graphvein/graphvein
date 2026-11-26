package typescript

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

func TestDeclarationMutationRoots(t *testing.T) {
	source := []byte(`
import Default from './arrays';
import { ARR as Named } from './arrays';
import * as Namespace from './arrays';
Before.push(1);
const Before = [];
export const Local = [];
const ObjectValue = { nested: [] };
let Mutable = [];
const First = [], Second = [];
Default.push(1);
Named[0]++;
delete Namespace['ARR'][0];
Local = [];
ObjectValue.nested.push(1);
Mutable.push(1);
First.push(1);
unknown.method();
this.method();
function run(Named: unknown, Namespace: unknown) {
  Named.nested.push(1);
  Namespace['ARR'][0] = 1;
  const Local = [];
  Local.push(1);
  Default.push(1);
}
`)
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	root := results[0].Root
	f := Extract("mutations.ts", source, root)
	syntax := CaptureDeclarations(root, f, CaptureTemplates(root, f.Nodes))
	counts := map[string]int{}
	for _, mutation := range syntax.Mutations {
		value := mutation.Value
		for value.Object != nil {
			value = *value.Object
		}
		counts[value.Name]++
	}
	want := map[string]int{"Before": 1, "Default": 2, "Named": 1, "Namespace": 1, "Local": 1, "ObjectValue": 1}
	if len(counts) != len(want) {
		t.Fatalf("mutation roots: %v, want %v", counts, want)
	}
	for name, count := range want {
		if counts[name] != count {
			t.Errorf("%s mutations: %d, want %d", name, counts[name], count)
		}
	}
}
