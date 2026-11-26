package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// TestArchitectureNotProvenOrder checks that the Not proven list (and the
// whole body) does not depend on map order (AG-S follow-up, 2026-11-18
// TOP-2): six components each with unlinked HTTP and IPC calls give twelve
// "not linked" entries, which two requests must return identically and in
// the ghosts' order (component, then family).
func TestArchitectureNotProvenOrder(t *testing.T) {
	w := &streamWriter{t: t, count: map[string]int{}}
	w.rec(map[string]any{"type": "header", "contract_version": 1, "indexer": map[string]any{"name": "crafted", "version": "top2"}, "repository": "archorder", "generated_at": "2026-11-19T00:00:00Z"})
	dirs := []string{"a", "b", "c", "d", "e", "f"}
	for _, dir := range dirs {
		for _, f := range []string{dir + "/package.json", dir + "/main.ts"} {
			w.rec(map[string]any{"type": "file", "path": f, "language": "unknown", "precision": "best_effort", "size": 0, "content_hash": contract.ContentHash(nil)})
		}
	}
	for _, dir := range dirs {
		file := dir + "/package.json"
		w.rec(map[string]any{"type": "node", "identity": w.id("package", file, dir), "kind": "package", "level": 2, "name": dir, "qualified_name": dir,
			"file": file, "span": w.span(), "content_hash": contract.ContentHash(nil),
			"attributes": map[string]any{"ecosystem": "npm", "dependencies": []any{}, "name": dir, "directory": path.Dir(file)}})
		w.node("client_call", dir+"/main.ts", "run.GET /x", map[string]any{"status": "unmatched", "paths": []any{}})
		w.node("ipc_call", dir+"/main.ts", "run.INVOKE name", map[string]any{"framework": "tauri", "status": "dynamic", "transport": "tauri_ipc"})
	}
	nodes := w.count["node"]
	w.rec(map[string]any{"type": "footer", "counts": map[string]any{"records": w.count["file"] + w.count["node"] + w.count["edge"], "file": w.count["file"], "node": w.count["node"], "edge": w.count["edge"], "diagnostic": 0}})
	backends(t, func(t *testing.T, write, read store.Store) {
		ingestStream(t, write, bytes.NewReader(w.buf.Bytes()), "archorder", nodes)
		opt := testOptions(t.TempDir(), read)
		opt.Repository = "archorder"
		var first []byte
		for i := 0; i < 2; i++ {
			rec := get(t, opt, http.MethodGet, "/api/v0/architecture", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d %s", rec.Code, rec.Body.String())
			}
			if i == 0 {
				first = rec.Body.Bytes()
				continue
			}
			if !bytes.Equal(first, rec.Body.Bytes()) {
				t.Fatalf("two requests returned different bodies:\n%s\n%s", first, rec.Body.Bytes())
			}
		}
		var body struct {
			NotProven []archNotProven `json:"not_proven"`
		}
		if err := json.Unmarshal(first, &body); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, np := range body.NotProven {
			if np.Kind == "calls_not_linked" {
				got = append(got, np.Component+" "+np.Row)
			}
		}
		var want []string
		for _, dir := range dirs {
			want = append(want, dir+" CE1", dir+" CE2")
		}
		if len(got) != len(want) {
			t.Fatalf("not linked entries %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("not linked entries %v, want %v", got, want)
			}
		}
	})
}
