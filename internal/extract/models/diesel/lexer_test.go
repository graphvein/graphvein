package diesel

import (
	"errors"
	"strings"
	"testing"
)

// tok is a compact view of a token for table tests.
func tokString(t Token) string {
	kinds := map[TokenKind]string{Ident: "id", Punct: "p", String: "str", Number: "num", Char: "char", Lifetime: "life", OuterDoc: "doc", InnerDoc: "idoc", Other: "other"}
	s := kinds[t.Kind] + ":" + t.Text
	if t.Raw {
		s += ":raw"
	}
	if t.Prefix != "" {
		s += ":" + t.Prefix
	}
	return s
}

func lexAll(t *testing.T, src string) []string {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var out []string
	for _, tk := range toks {
		out = append(out, tokString(tk))
	}
	return out
}

func TestLexTokenForms(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{"idents and paths", "diesel::table _x Ünï", []string{"id:diesel", "p:::", "id:table", "id:_x", "id:Ünï"}},
		{"raw identifier", "r#type r#match", []string{"id:type:raw", "id:match:raw"}},
		{"joined punctuation", "a -> b => c :: d", []string{"id:a", "p:->", "id:b", "p:=>", "id:c", "p:::", "id:d"}},
		{"generics stay single", "Nullable<Array<Text>>", []string{"id:Nullable", "p:<", "id:Array", "p:<", "id:Text", "p:>", "p:>"}},
		{"single punctuation", "#[]!,;.=*&+-/@$?|^%~:", []string{"p:#", "p:[", "p:]", "p:!", "p:,", "p:;", "p:.", "p:=", "p:*", "p:&", "p:+", "p:-", "p:/", "p:@", "p:$", "p:?", "p:|", "p:^", "p:%", "p:~", "p::"}},
		{"string with escapes", `"a\"b\\c\n\t\r\0\x41\u{e9}\'"`, []string{"str:a\"b\\c\n\t\r\x00Aé'"}},
		{"string line continuation", "\"a\\\n    b\"", []string{"str:ab"}},
		{"string CRLF", "\"a\r\nb\"", []string{"str:a\nb"}},
		{"raw strings", `r"a\n" r#"x"y"# r##"a"#b"##`, []string{`str:a\n:r`, `str:x"y:r`, `str:a"#b:r`}},
		{"byte and C strings", `b"ab" br"x" c"y" cr#"z"#`, []string{"str:ab:b", "str:x:br", "str:y:c", "str:z:cr"}},
		{"numbers", "255 1_000u32 0x1F 0o7 0b10 1.5 1e3 2.0f64", []string{"num:255", "num:1_000u32", "num:0x1F", "num:0o7", "num:0b10", "num:1.5", "num:1e3", "num:2.0f64"}},
		{"range is not a float", "1..2", []string{"num:1", "p:.", "p:.", "num:2"}},
		{"chars and lifetimes", `'a' '\n' '\'' b'x' 'static '_`, []string{"char:'a'", `char:'\n'`, `char:'\''`, "char:b'x'", "life:'static", "life:'_"}},
		{"line and block comments dropped", "a // c\n b /* x /* nested */ y */ c", []string{"id:a", "id:b", "id:c"}},
		{"doc comments kept", "/// outer\n//! inner\n/** block */\n/*! iblock */\nx", []string{"doc: outer", "idoc: inner", "doc: block ", "idoc: iblock ", "id:x"}},
		{"not doc comments", "//// four\n/**/ /*** three */ x", []string{"id:x"}},
		{"other character", "a ` b", []string{"id:a", "other:`", "id:b"}},
		{"byte order mark", "\ufeffx", []string{"id:x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lexAll(t, tc.src)
			if strings.Join(got, " | ") != strings.Join(tc.want, " | ") {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestLexPositions(t *testing.T) {
	toks, err := Lex("a\n  /* x\n y */ b\n\"s\ntring\" c")
	if err != nil {
		t.Fatal(err)
	}
	want := [][3]int{{1, 1}, {3, 7}, {4, 1}, {5, 8}}
	for i, w := range want {
		if toks[i].Line != w[0] || toks[i].Col != w[1] {
			t.Errorf("token %d %q at %d:%d, want %d:%d", i, toks[i].Text, toks[i].Line, toks[i].Col, w[0], w[1])
		}
	}
}

func TestLexRefusals(t *testing.T) {
	cases := []struct {
		name, src string
		line      int
	}{
		{"unterminated string", "a\n\"abc", 2},
		{"unterminated raw string", `r#"abc"`, 1},
		{"unterminated block comment", "x /* a /* b */", 1},
		{"invalid escape", `"\q"`, 1},
		{"invalid hex escape", `"\xZZ"`, 1},
		{"invalid unicode escape", `"\u{110000}"`, 1},
		{"unterminated char", "'", 1},
		{"not UTF-8", "a\xff", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Lex(tc.src)
			var le *LexError
			if !errors.As(err, &le) {
				t.Fatalf("Lex(%q) = %v, want a LexError", tc.src, err)
			}
			if le.Line != tc.line {
				t.Errorf("line %d, want %d (%v)", le.Line, tc.line, err)
			}
		})
	}
}

func TestTrees(t *testing.T) {
	trees, err := LexTrees("a!{ b ( c [ d ] ) }\nx")
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 4 || trees[2].Kind != Group || trees[2].Delim != '{' {
		t.Fatalf("trees %+v", trees)
	}
	inner := trees[2].Kids
	if len(inner) != 2 || inner[1].Delim != '(' || inner[1].Kids[1].Delim != '[' || inner[1].Kids[1].Kids[0].Text != "d" {
		t.Errorf("nested groups %+v", inner)
	}
	for _, src := range []string{"a ( b", "a ) b", "( ]", "{ ( } )"} {
		if _, err := LexTrees(src); err == nil {
			t.Errorf("LexTrees(%q): want an unbalanced-delimiter error", src)
		}
	}
}
