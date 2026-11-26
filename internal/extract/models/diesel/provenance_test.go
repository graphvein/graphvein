package diesel

import (
	"reflect"
	"strings"
	"testing"
)

// generatedRS is a print-schema shaped file: header, two tables and a
// joinable!, 22 lines.
const generatedRS = `// @generated automatically by Diesel CLI.

diesel::table! {
    posts (id) {
        id -> Int4,
        title -> Text,
        body -> Text,
        user_id -> Int4,
    }
}

diesel::table! {
    users (id) {
        id -> Int4,
        name -> Text,
    }
}

diesel::joinable!(posts -> users (user_id));

diesel::allow_tables_to_appear_in_same_query!(posts, users,);
`

// patchedRS is generatedRS after patchFor: body's type changed, a
// column removed from users, a joinable! added.
const patchedRS = `// @generated automatically by Diesel CLI.

diesel::table! {
    posts (id) {
        id -> Int4,
        title -> Text,
        body -> Nullable<Text>,
        user_id -> Int4,
    }
}

diesel::table! {
    users (id) {
        id -> Int4,
    }
}

diesel::joinable!(posts -> users (user_id));
diesel::joinable!(users -> posts (id));

diesel::allow_tables_to_appear_in_same_query!(posts, users,);
`

const schemaPatch = `diff --git a/src/schema.rs b/src/schema.rs
--- a/src/schema.rs
+++ b/src/schema.rs
@@ -4,7 +4,7 @@
     posts (id) {
         id -> Int4,
         title -> Text,
-        body -> Text,
+        body -> Nullable<Text>,
         user_id -> Int4,
     }
 }
@@ -12,10 +12,10 @@
 diesel::table! {
     users (id) {
         id -> Int4,
-        name -> Text,
     }
 }

 diesel::joinable!(posts -> users (user_id));
+diesel::joinable!(users -> posts (id));

 diesel::allow_tables_to_appear_in_same_query!(posts, users,);
`

func configuredFile(patch string) *File {
	return &File{Path: "src/schema.rs", Configured: true, Config: &Config{Path: "diesel.toml"},
		Section: &Section{Key: "default", Written: true, File: "src/schema.rs", Path: "src/schema.rs", PatchFile: patch, PatchPath: patch}}
}

func provenance(t *testing.T, schema string, files map[string]string, patch string) (*Provenance, []string) {
	t.Helper()
	p, diags := ReadProvenance(Input{Sources: sources(files)}, configuredFile(patch), []byte(schema))
	return p, messages(diags)
}

func TestProvenanceHeaderAndNoPatch(t *testing.T) {
	p, diags := provenance(t, generatedRS, nil, "")
	if !p.Generated || p.Reason != "" || len(diags) != 0 || p.HandWritten(5) {
		t.Errorf("generated file: %+v %q", p, diags)
	}
	p, _ = provenance(t, strings.Replace(generatedRS, "CLI.", "CLI", 1), nil, "")
	if p.Generated || !p.HandWritten(5) || !strings.Contains(p.Reason, "header") {
		t.Errorf("no header: %+v", p)
	}
	// The header must be the first line.
	p, _ = provenance(t, "\n"+generatedRS, nil, "")
	if p.Generated {
		t.Errorf("header on line 2 read as generated")
	}
	// An unconfigured file is hand-written whatever it says.
	q, _ := ReadProvenance(Input{}, &File{Path: "src/schema.rs"}, []byte(generatedRS))
	if q.Generated {
		t.Errorf("unconfigured file read as generated")
	}
}

func TestProvenancePatch(t *testing.T) {
	p, diags := provenance(t, patchedRS, map[string]string{"src/schema.patch": schemaPatch}, "src/schema.patch")
	if !p.Generated || len(diags) != 0 {
		t.Fatalf("%+v %q", p, diags)
	}
	added := []int{}
	for l := 1; l <= 25; l++ {
		if p.HandWritten(l) {
			added = append(added, l)
		}
	}
	if !reflect.DeepEqual(added, []int{7, 19}) {
		t.Errorf("hand-written lines %v, want [7 19]", added)
	}
	if len(p.Removals) != 2 {
		t.Fatalf("removals %+v", p.Removals)
	}
	// body -> Text is replaced under the same name: benign. name -> Text
	// is removed from users (lines 12-16): its column set is not complete.
	if r := p.Removals[0]; !r.Benign || r.After != 6 {
		t.Errorf("first removal %+v", r)
	}
	if r := p.Removals[1]; r.Benign || r.After != 14 {
		t.Errorf("second removal %+v", r)
	}
	if p.RemovedWithin(3, 10) || !p.RemovedWithin(12, 16) || p.RemovesTable() {
		t.Errorf("RemovedWithin posts %v users %v, RemovesTable %v", p.RemovedWithin(3, 10), p.RemovedWithin(12, 16), p.RemovesTable())
	}
}

func TestProvenanceOffset(t *testing.T) {
	// Five lines inserted above the patched region since the patch was
	// written: both hunks apply 5 lines later, the second also -1 from
	// the first hunk's removal.
	extra := "// a\n// b\n// c\n// d\n// e\n"
	shifted := strings.Replace(patchedRS, "\ndiesel::table! {\n    posts", "\n"+extra+"diesel::table! {\n    posts", 1)
	p, diags := provenance(t, shifted, map[string]string{"src/schema.patch": schemaPatch}, "src/schema.patch")
	if !p.Generated || len(diags) != 0 {
		t.Fatalf("%+v %q", p, diags)
	}
	var offsets []int
	for _, h := range p.Hunks {
		offsets = append(offsets, h.Offset())
	}
	if !reflect.DeepEqual(offsets, []int{5, 5}) || !p.HandWritten(12) || !p.HandWritten(24) || p.HandWritten(7) {
		t.Errorf("offsets %v added %v", offsets, p.Added)
	}
	// Lines removed above: a negative offset.
	up := strings.Replace(patchedRS, "// @generated automatically by Diesel CLI.\n\n", "// @generated automatically by Diesel CLI.\n", 1)
	p, _ = provenance(t, up, map[string]string{"src/schema.patch": schemaPatch}, "src/schema.patch")
	if !p.Generated || p.Hunks[0].Offset() != -1 || !p.HandWritten(6) {
		t.Errorf("negative offset: %+v", p)
	}
}

func TestProvenanceUnlocatable(t *testing.T) {
	cases := map[string]struct {
		schema, patch, want string
	}{
		"hunk not in the file": {strings.Replace(patchedRS, "body -> Nullable<Text>", "body -> Nullable<Varchar>", 1), schemaPatch, "hunk 1 (patch line 4, +4) does not match the file"},
		"fuzz is not applied":  {strings.Replace(patchedRS, "        title -> Text,\n", "        title -> Varchar,\n", 1), schemaPatch, "does not match"},
		"two places": {patchedRS + "\ndiesel::joinable!(posts -> users (user_id));\ndiesel::joinable!(users -> posts (id));\n\n",
			"--- a\n+++ b\n@@ -19,2 +19,3 @@\n diesel::joinable!(posts -> users (user_id));\n+diesel::joinable!(users -> posts (id));\n \n", "matches the file at lines 18 and 23"},
		"removal only, no context": {patchedRS, "--- a\n+++ b\n@@ -14,1 +13,0 @@\n-        name -> Text,\n", "no context or added lines"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, diags := provenance(t, tc.schema, map[string]string{"src/schema.patch": tc.patch}, "src/schema.patch")
			if p.Generated || !p.HandWritten(1) || !strings.Contains(p.Reason, tc.want) || len(diags) != 1 || !strings.Contains(diags[0], "names only (DI-G1)") {
				t.Errorf("%+v %q, want hand-written: %s", p, diags, tc.want)
			}
		})
	}
}

func TestProvenancePatchFileProblems(t *testing.T) {
	cases := map[string]struct {
		files       map[string]string
		path, patch string
		want        string
	}{
		"missing file":      {nil, "src/schema.patch", "src/schema.patch", "src/schema.patch does not exist"},
		"outside the repo":  {nil, "../x.patch", "", `"../x.patch" cannot be placed in the repository`},
		"not UTF-8":         {map[string]string{"src/schema.patch": "\xff\xfe"}, "src/schema.patch", "src/schema.patch", "not UTF-8 text"},
		"not a diff":        {map[string]string{"src/schema.patch": "@@ -1 +1\n"}, "src/schema.patch", "src/schema.patch", "hunk header unterminated"},
		"two files":         {map[string]string{"src/schema.patch": schemaPatch + "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n"}, "src/schema.patch", "src/schema.patch", "is not a unified diff"},
		"adds the header":   {map[string]string{"src/schema.patch": "--- a\n+++ b\n@@ -0,0 +1,1 @@\n+// @generated automatically by Diesel CLI.\n"}, "src/schema.patch", "src/schema.patch", "adds the header line itself"},
		"counts mismatched": {map[string]string{"src/schema.patch": "--- a\n+++ b\n@@ -1,2 +1,2 @@\n // @generated automatically by Diesel CLI.\n"}, "src/schema.patch", "src/schema.patch", "hunk header does not match hunk"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := configuredFile(tc.path)
			f.Section.PatchPath = tc.patch
			p, diags := ReadProvenance(Input{Sources: sources(tc.files)}, f, []byte(generatedRS))
			if p.Generated || !strings.Contains(p.Reason, tc.want) || len(diags) != 1 {
				t.Errorf("%+v %q, want %q", p, messages(diags), tc.want)
			}
		})
	}
}

func TestProvenanceCRLF(t *testing.T) {
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	want, _ := provenance(t, patchedRS, map[string]string{"src/schema.patch": schemaPatch}, "src/schema.patch")
	for name, tc := range map[string][2]string{
		"both CRLF":  {crlf(patchedRS), crlf(schemaPatch)},
		"patch CRLF": {patchedRS, crlf(schemaPatch)},
		"file CRLF":  {crlf(patchedRS), schemaPatch},
	} {
		got, diags := provenance(t, tc[0], map[string]string{"src/schema.patch": tc[1]}, "src/schema.patch")
		if !reflect.DeepEqual(got, want) || len(diags) != 0 {
			t.Errorf("%s: %+v %q, want %+v", name, got, diags, want)
		}
	}
	p, _ := provenance(t, crlf(generatedRS), nil, "")
	if !p.Generated {
		t.Errorf("CRLF header not recognised")
	}
}

func TestProvenanceNoNewlineAtEnd(t *testing.T) {
	schema := strings.TrimSuffix(patchedRS, "\n")
	patch := "--- a\n+++ b\n@@ -20,3 +20,4 @@\n diesel::joinable!(posts -> users (user_id));\n+diesel::joinable!(users -> posts (id));\n \n diesel::allow_tables_to_appear_in_same_query!(posts, users,);\n\\ No newline at end of file\n"
	p, diags := provenance(t, schema, map[string]string{"src/schema.patch": patch}, "src/schema.patch")
	if !p.Generated || !p.HandWritten(19) || len(diags) != 0 {
		t.Errorf("%+v %q", p, diags)
	}
	// The same hunk does not match a file that ends with a line break.
	p, _ = provenance(t, patchedRS, map[string]string{"src/schema.patch": patch}, "src/schema.patch")
	if p.Generated {
		t.Errorf("a no-newline hunk matched a terminated last line")
	}
}

func TestProvenanceRemovesTable(t *testing.T) {
	schema := strings.Replace(generatedRS, "diesel::table! {\n    users (id) {\n        id -> Int4,\n        name -> Text,\n    }\n}\n\n", "", 1)
	patch := "--- a\n+++ b\n@@ -9,13 +9,6 @@\n     }\n }\n \n-diesel::table! {\n-    users (id) {\n-        id -> Int4,\n-        name -> Text,\n-    }\n-}\n-\n diesel::joinable!(posts -> users (user_id));\n \n diesel::allow_tables_to_appear_in_same_query!(posts, users,);\n"
	p, diags := provenance(t, schema, map[string]string{"src/schema.patch": patch}, "src/schema.patch")
	if !p.Generated || !p.RemovesTable() || len(diags) != 0 {
		t.Errorf("%+v %q", p, diags)
	}
}

func TestParsePatch(t *testing.T) {
	p, err := ParsePatch("preamble\nindex 1..2\n--- \"a/x\\ty\"\t2025-01-01\n+++ b/x\n@@ -1,2 +1,3 @@ fn context\n a\n+b\n\n@@ -5,0 +7 @@\n+c\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hunks) != 2 {
		t.Fatalf("hunks %+v", p.Hunks)
	}
	// An empty line inside a hunk is a context line, as diffy reads it.
	h := p.Hunks[0]
	if h.Line != 5 || h.NewLen != 3 || len(h.Lines) != 3 || h.Lines[2] != (PatchLine{' ', "\n"}) {
		t.Errorf("first hunk %+v", h)
	}
	if h := p.Hunks[1]; h.Line != 9 || h.OldStart != 5 || h.OldLen != 0 || h.NewStart != 7 || h.NewLen != 1 {
		t.Errorf("second hunk %+v", h)
	}
	for name, src := range map[string]string{
		"two old names":     "--- a\n--- b\n",
		"bad quoted name":   "--- \"a\\q\"\n",
		"unquoted quote":    "--- a\"b\n",
		"unterminated name": "--- a",
		"bad range":         "@@ -x +1 @@\n",
		"negative range":    "@@ -1,-1 +1 @@\n",
		"stray line":        "@@ -1 +1 @@\n-a\n+b\n?\n",
		"out of order":      "@@ -5 +5 @@\n-a\n+b\n@@ -1 +1 @@\n-a\n+b\n",
		"no newline first":  "@@ -1 +1 @@\n\\ No newline at end of file\n",
		"after no newline":  "@@ -1,2 +1,2 @@\n a\n\\ No newline at end of file\n b\n",
	} {
		if _, err := ParsePatch(src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// No hunks at all is a patch that changes nothing.
	if p, err := ParsePatch("just text\n"); err != nil || len(p.Hunks) != 0 {
		t.Errorf("empty patch: %v %v", p, err)
	}
}

func TestLocateHunksReplay(t *testing.T) {
	// The new side "y" sits only at line 3, but applying the patch to the
	// text that location implies (x, a, x) would replace the first x:
	// diffy could not have written this file with this patch.
	p, err := ParsePatch("@@ -2 +2 @@\n-x\n+y\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LocateHunks("x\na\ny\n", p); err == nil || !strings.Contains(err.Error(), "does not place its hunks") {
		t.Errorf("replay accepted: %v", err)
	}
	if l, err := LocateHunks("y\na\nx\n", p); err != nil || l[0].Line != 1 || l[0].Offset() != -1 {
		t.Errorf("located %+v %v", l, err)
	}
}
