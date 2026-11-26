package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestSettingsReviewFixes checks the T039 fixes on the server
// side: the decoded value of a non-decimal integer (rows ST-12 and
// ST-24), the key that locks ide.editor with its line (row ST-2),
// a lost comment after an apostrophe in a plain scalar (row ST-27),
// the change line of a whole-text edit of a file that did not load
// (row ST-28) and what a re-encode changes beyond the indentation.
func TestSettingsReviewFixes(t *testing.T) {
	ss := newSettingsServer(t, "version: 1\nindex:\n  shape_depth: 0x7\nide:\n  editor: cursor\n  link_template: \"myed://open?f={abs_path}&l={line}\"\n")
	doc := ss.get("")
	if k := keyByName(t, doc, "index.shape_depth"); k.Value != float64(7) {
		t.Errorf("shape_depth 0x7 shown as %v", k.Value)
	}
	editor := keyByName(t, doc, "ide.editor")
	if editor.Editable || editor.LockedBy == nil || *editor.LockedBy != (configLockDoc{Key: "ide.link_template", Line: 6}) {
		t.Errorf("locked editor %+v (locked_by %+v)", editor, editor.LockedBy)
	}
	if k := keyByName(t, doc, "ui.labels"); k.LockedBy != nil {
		t.Errorf("ui.labels locked by %+v", k.LockedBy)
	}
	code, preview, msg := ss.post("", editBody(*doc.Hash, "text", `"version: 1\nindex:\n  shape_depth: 7\nide:\n  editor: cursor\n  link_template: \"myed://open?f={abs_path}&l={line}\"\n"`, ""))
	if code != http.StatusOK || len(preview.ChangedKeys) != 0 || preview.ReindexNeeded {
		t.Errorf("0x7 -> 7 is no change: %d %+v %s", code, preview, msg)
	}

	ss = newSettingsServer(t, "version: 1\nrepository: rock 'n roll\ntests:\n  # why include\n  include: [\"a\"]\n  exclude: [\"b\"]\n")
	code, preview, msg = ss.post("", editBody(*ss.get("").Hash, "operations", `[{"op":"remove","key":"tests.include"}]`, ""))
	if code != http.StatusOK || len(preview.LostComments) != 1 || preview.LostComments[0].Text != "# why include" {
		t.Errorf("apostrophe: %d %+v %s", code, preview.LostComments, msg)
	}

	ss = newSettingsServer(t, "version: 1\nui:\n  labels: fancy\n")
	_, preview, _ = ss.post("", editBody(*ss.get("").Hash, "text", `"version: 1\n"`, ""))
	if !reflect.DeepEqual(preview.Changes, []string{"replaces graphvein.yaml with the text given", "the file did not load before; every key is treated as changed"}) {
		t.Errorf("fixing a broken file: %q", preview.Changes)
	}

	ss = newSettingsServer(t, "\xEF\xBB\xBFversion: 1\r\nrepository: &r x\r\ntests: {include: [a]}\r\n")
	_, preview, _ = ss.post("", editBody(*ss.get("").Hash, "operations", `[{"op":"set","key":"tests.include","value":["b"]}]`, ""))
	words := strings.Join(preview.Changes, "; ")
	if !preview.Reformatted || !strings.Contains(words, "drops the byte order mark") || !strings.Contains(words, "from CRLF to LF") {
		t.Errorf("re-encode words %q", preview.Changes)
	}
}
