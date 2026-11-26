//go:build graphveinprofile

package sqlstore

import (
	"strings"
	"testing"
)

func TestProfileMapSharedStrings(t *testing.T) {
	key, payload := "path", "payload"
	tx := &transaction{old: map[string]map[string]string{"files": {key: payload}}, seen: map[string]map[string]bool{"files": {key: true}}}
	stats := profileMapStrings("test", tx)
	if stats.UniqueStringBytes != uint64(len(key)+len(payload)) {
		t.Fatalf("shared key counted twice: %+v", stats)
	}
	tx.seen["files"] = map[string]bool{strings.Clone(key): true}
	stats = profileMapStrings("test", tx)
	if stats.UniqueStringBytes != uint64(2*len(key)+len(payload)) {
		t.Fatalf("separate key backing not counted: %+v", stats)
	}
	if stats.OldPayloadBytes != uint64(len(payload)) || stats.OldRows[0] != 1 || stats.SeenRows[0] != 1 {
		t.Fatalf("incorrect map inventory: %+v", stats)
	}
}
