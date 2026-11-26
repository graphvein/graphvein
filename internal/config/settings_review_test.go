package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestSettingValuesDecodedIntegers checks rows ST-12 and
// ST-43: index.shape_depth in a non-decimal integer form is shown,
// compared and recorded as the strict decoder reads it, so the current
// side of the freshness comparison equals what a run records.
func TestSettingValuesDecodedIntegers(t *testing.T) {
	for _, c := range []struct {
		text string
		want int
	}{
		{"010", 8},
		{"0x7", 7},
		{"0o7", 7},
		{"1_0", 10},
		{"6", 6},
	} {
		data := []byte("version: 1\nindex:\n  shape_depth: " + c.text + "\n")
		cfg, err := Parse(File, data)
		if err != nil {
			t.Fatalf("%s does not load: %v", c.text, err)
		}
		if cfg.Index.ShapeDepth != c.want {
			t.Fatalf("decoder reads %s as %d, want %d", c.text, cfg.Index.ShapeDepth, c.want)
		}
		values, _, err := SettingValues(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if v.Key.Key == "index.shape_depth" && v.Value != c.want {
				t.Errorf("%s shown as %v, want %d", c.text, v.Value, c.want)
			}
		}
		// The run records the decoded depth (WithShapeDepth from the
		// decoded config); the current side must agree.
		in, err := IndexInputsOf(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := CompareIndexInputs(in.WithShapeDepth(cfg.Index.ShapeDepth).Record(), in); got.State != "unchanged" {
			t.Errorf("%s: a fresh run's record compares as %+v", c.text, got)
		}
	}
	keys := func(a, b string) []string {
		ks, _, err := ChangedSettings([]byte("version: 1\nindex:\n  shape_depth: "+a+"\n"), []byte("version: 1\nindex:\n  shape_depth: "+b+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, k := range ks {
			out = append(out, k.Key)
		}
		return out
	}
	if got := keys("0x5", "0x7"); !reflect.DeepEqual(got, []string{"index.shape_depth"}) {
		t.Errorf("0x5 -> 0x7 changes %v", got)
	}
	if got := keys("0x6", "6"); got != nil {
		t.Errorf("0x6 -> 6 changes %v", got)
	}
	words, err := SettingChangeWords([]byte("version: 1\nindex:\n  shape_depth: 0x7\n"), []SettingOp{{Key: "index.shape_depth", Value: 8}})
	if err != nil || !reflect.DeepEqual(words, []string{"sets index.shape_depth to 8 (was 7)"}) {
		t.Errorf("words %q %v", words, err)
	}
}

func commentTexts(cs []Comment) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}

// TestCommentsReview checks row ST-27:
// the comments come from yaml.v3's parser, so a quote inside a plain
// scalar, a byte order mark and a # inside a block scalar are read as
// YAML reads them, and lost_comments sees every lost comment.
func TestCommentsReview(t *testing.T) {
	apostrophe := []byte("version: 1\nrepository: rock 'n roll\ntests:\n  # why include\n  include: [\"a\"]\n  exclude: [\"b\"]\n")
	got, _, err := EditSettings(apostrophe, []SettingOp{{Key: "tests.include", Remove: true}})
	if err != nil {
		t.Fatal(err)
	}
	if lost := LostComments(apostrophe, got); !reflect.DeepEqual(lost, []Comment{{Line: 4, Text: "# why include"}}) {
		t.Errorf("apostrophe: lost %+v in\n%s", lost, got)
	}
	quote := []byte("version: 1\nrepository: say \"hi\n# kept?\nui:\n  labels: plain # style\n")
	if c := Comments(quote); !reflect.DeepEqual(c, []Comment{{3, "# kept?"}, {5, "# style"}}) {
		t.Errorf("double quote in a plain scalar: %+v", c)
	}
	bom := []byte("\xEF\xBB\xBF# keep me\nversion: 1\n")
	if lost := LostComments(bom, []byte("version: 1\n")); !reflect.DeepEqual(lost, []Comment{{1, "# keep me"}}) {
		t.Errorf("BOM: lost %+v", lost)
	}
	block := []byte("version: 1\nrepository: |\n  a # not a comment\n  # nor this\n# real\nui:\n  labels: plain\n")
	if c := Comments(block); !reflect.DeepEqual(c, []Comment{{5, "# real"}}) {
		t.Errorf("block scalar: %+v", c)
	}
	if lost := LostComments(block, []byte("version: 1\nrepository: |\n  a # not a comment\n  # nor this\nui:\n  labels: plain\n")); !reflect.DeepEqual(lost, []Comment{{5, "# real"}}) {
		t.Errorf("block scalar: lost %+v", lost)
	}
	// A text that does not parse is read by the line scanner, which
	// skips the BOM and opens a quoted scalar only at a scalar's start.
	broken := []byte("\xEF\xBB\xBF# head\nrepository: rock 'n roll # one\nui: [plain\n# two\n")
	if c := commentTexts(Comments(broken)); !reflect.DeepEqual(c, []string{"# head", "# one", "# two"}) {
		t.Errorf("scanner: %q", c)
	}
	if lost := LostComments(broken, []byte("repository: rock 'n roll\nui: [plain\n# two\n")); !reflect.DeepEqual(commentTexts(lost), []string{"# head", "# one"}) {
		t.Errorf("scanner lost %+v", lost)
	}
}

// TestReencodeWords checks that a re-encode says when it drops a byte
// order mark, CRLF line endings or an anchor (row ST-27's change line).
func TestReencodeWords(t *testing.T) {
	before := []byte("\xEF\xBB\xBFversion: 1\r\nrepository: &r demo\r\nexplorer:\r\n  path_values: {}\r\n")
	got := ReencodeWords(before, []byte("version: 1\nrepository: demo\nexplorer:\n  path_values: {}\n"))
	want := []string{
		"drops the byte order mark at the start of graphvein.yaml",
		"changes the line endings of graphvein.yaml from CRLF to LF",
		"drops the anchor &r (line 2)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("words %q", got)
	}
	if got := ReencodeWords([]byte("a: &x 1\n"), []byte("a: &x 1\n")); len(got) != 0 {
		t.Errorf("nothing dropped: %q", got)
	}
	// The re-encode of a CRLF file with a flow section (an edit the
	// splice cannot prove) says what it changes.
	data := []byte("version: 1\r\nrepository: x\r\ntests: {include: [a]}\r\n")
	out, reformatted, err := EditSettings(data, []SettingOp{{Key: "tests.include", Value: []string{"b"}}})
	if err != nil || !reformatted {
		t.Fatalf("%v %v\n%s", reformatted, err, out)
	}
	if _, err := Parse(File, out); err != nil {
		t.Fatalf("result does not load: %v", err)
	}
	if w := strings.Join(ReencodeWords(data, out), "; "); !strings.Contains(w, "CRLF to LF") {
		t.Errorf("re-encode words %q for\n%q", w, out)
	}
}
