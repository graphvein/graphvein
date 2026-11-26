package python

import (
	"strings"
	"testing"
)

// TestPythonRejectionForms checks each PS-F2 rejection form on minimal
// sources (every form has a case; the review's probes among them)
// and the accepted neighbours each must not reject.
func TestPythonRejectionForms(t *testing.T) {
	deep := strings.Repeat("(", 201) + "1" + strings.Repeat(")", 201)
	deepOK := strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200)
	var indented, indentedOK strings.Builder
	for i := 0; i < 100; i++ {
		indented.WriteString(strings.Repeat(" ", i) + "if x:\n")
	}
	indented.WriteString(strings.Repeat(" ", 100) + "pass\n")
	for i := 0; i < 99; i++ {
		indentedOK.WriteString(strings.Repeat(" ", i) + "if x:\n")
	}
	indentedOK.WriteString(strings.Repeat(" ", 99) + "pass\n")
	cases := []struct{ source, want string }{
		{"x = = 1\n", rejectTreeSitterError},
		{"print 'x'\n", rejectPrintStatement},
		{"exec 'x'\n", rejectExecStatement},
		{"raise E, \"msg\"\n", rejectRaiseComma},
		{"raise E, V, T\n", rejectRaiseComma},
		{"a <> b\n", rejectDiamond},
		{"x = `y`\n", rejectBacktick},
		{"x = ur'y'\n", rejectStringPrefix},
		{"x = b'\xc3\xa9'\n", rejectStringLiteral},
		{"x = 'a' b'c'\n", rejectStringLiteral},
		{"x = t'a' 'b'\n", rejectStringLiteral},
		{"x = f'{a!x}'\n", rejectStringLiteral},
		{"x = '\\N{NO SUCH NAME}'\n", rejectStringLiteral},
		// `\N{...}` names fold ASCII letters only (CPython's Py_TOUPPER):
		// U+017F, U+0131 and U+212A upper-case to S, I and K in Unicode.
		{"x = '\\N{LATIN \u017fMALL LETTER A}'\n", rejectStringLiteral},
		{"x = '\\N{lat\u0131n small letter a}'\n", rejectStringLiteral},
		{"x = '\\N{\u212aELVIN SIGN}'\n", rejectStringLiteral},
		{"x = '\\N{ARABIC LETTER NOON WITH RING ABOV\u212a}'\n", rejectStringLiteral},
		{"x = 0777\n", rejectLegacyInteger},
		{"x = 12L\n", rejectLegacyInteger},
		{"x = 1_\n", rejectNumberLiteral},
		{"x = 1_.5\n", rejectNumberLiteral},
		{"try:\n    pass\nexcept A, B as e:\n    pass\n", rejectExceptComma},
		{"try:\n    pass\nexcept* A:\n    pass\nexcept B:\n    pass\n", rejectExceptStar},
		{"try:\n    pass\nexcept*:\n    pass\n", rejectExceptStar},
		{"def f(a=1, b): pass\n", rejectParameterOrder},
		{"def f(*): pass\n", rejectParameterOrder},
		{"def f(*, **k): pass\n", rejectParameterOrder},
		{"def f(**k, a): pass\n", rejectParameterOrder},
		{"def f(*a, *b): pass\n", rejectParameterOrder},
		{"def f(**a, **b): pass\n", rejectParameterOrder},
		{"def f(*, a, /): pass\n", rejectParameterOrder},
		{"def f(/, a): pass\n", rejectParameterOrder},
		{"def f(a, /, /): pass\n", rejectParameterOrder},
		{"def f(*, **kw: str): pass\n", rejectParameterOrder},
		{"f(a=1, b)\n", rejectArgumentOrder},
		{"f(**k, *a)\n", rejectArgumentOrder},
		{"f(**k, b)\n", rejectArgumentOrder},
		{"f(x for x in y, 1)\n", rejectComprehensionIn},
		{"[x for x in 1, 2]\n", rejectComprehensionIn},
		{"x := 1\n", rejectBareWalrus},
		{"del f()\n", rejectInvalidTarget},
		{"del 1\n", rejectInvalidTarget},
		{"del x + 1\n", rejectInvalidTarget},
		{"del (a, f())\n", rejectInvalidTarget},
		{"del (f())\n", rejectInvalidTarget},
		{"del *a\n", rejectInvalidTarget},
		{"with a as f(): pass\n", rejectInvalidTarget},
		{"(x, y): int = 1, 2\n", rejectInvalidTarget},
		{"[a]: int\n", rejectInvalidTarget},
		// Identifiers whose NFKC form is a keyword constant, in any
		// position (mathematical bold, full-width, modifier letters).
		{"def \U0001d413\U0001d42b\U0001d42e\U0001d41e(): pass\n", rejectConstantName},
		{"x = a.\U0001d40d\U0001d428\U0001d427\U0001d41e\n", rejectConstantName},
		{"f(\U0001d405alse=1)\n", rejectConstantName},
		{"import \U0001d413rue\n", rejectConstantName},
		{"x = \uff34rue\n", rejectConstantName},
		{"x = Tru\u1d49\n", rejectConstantName},
		{"x = f'{\U0001d40done}'\n", rejectConstantName},
		{"match x:\n    case \U0001d413rue:\n        pass\n", rejectConstantName},
		{"a, b += 1\n", rejectAugmentedUnpacked},
		{"from x import a,\n", rejectImportFromList},
		{"from a import b.c\n", rejectImportFromList},
		{"from a import b.c as d\n", rejectImportFromList},
		{"if x:\n\tif y:\n        pass\n", rejectIndentation},
		{"if x:\n        a = 1\n\tb = 2\n", rejectIndentation},
		{deep + "\n", rejectNestingLimit},
		{indented.String(), rejectNestingLimit},
		{"\xef\xbb\xbf# coding: latin-1\ndef f(): pass\n", rejectEncodingCookie},
		{"# coding: foobar\ndef f(): pass\n", rejectEncodingCookie},
		{"# -*- coding: latin-1 -*-\ndef caf\xc3\xa9(): pass\n", rejectEncodingCookie},
		{"#!/usr/bin/env python\n# vim: set fileencoding=cp1252 :\nx = '\xc3\xa9'\n", rejectEncodingCookie},
		{"# coding: unicode_escape\nx = 1\n", rejectEncodingCookie},
		{"x = '\\N{ARABIC LETTER NOON WITH RING ABOVE}'\n", rejectUnicodeVersion},
		{"x = '\\N{telugu archaic shrii}'\n", rejectUnicodeVersion},
		// Accepted by Python 3.14.
		{"try:\n    pass\nexcept A, B:\n    pass\n", ""},
		{"try:\n    pass\nexcept* A:\n    pass\nexcept* (B, C) as e:\n    pass\n", ""},
		{"x = t'y {z}' t'w'\n", ""},
		{"x = rb'y' + Rb'z' + f'{a}' + u'q'\n", ""},
		{"x = 'a' f'{b!r:>{w}}' 'c'\n", ""},
		{"x = b'a' rb'\\N{x}' + r'\\N{x}' + '\\\\N{x}'\n", ""},
		{"x = '\\N{BULLET}\\N{latin small letter a}'\n", ""},
		{"x = '\\N{LATIN LETTER PHARYNGEAL VOICED FRICATIVE}'\n", ""},
		{"\u0295 = 1\n", ""},
		{"x = True or False or None\n\U0001d413rue_ = \U0001d413ru = 1\n", ""},
		{"x = '\xc3\xa9'\n", ""},
		{"x = 0 + 00 + 0_0 + 0x1F + 0o17 + 0b1 + 0.5 + 0e1 + 0j + 10 + 1_000 + 0x_ff + 0b_1 + 1_0.5_5e1_0j\n", ""},
		{"def f(a, b=1, /, c=2, *args, d, e=3, **k): pass\n", ""},
		{"def f(a=1, *, b): pass\n", ""},
		{"def f(a, /): pass\n", ""},
		{"def f(self, *args: int, b: int | None = None, **kw: str): pass\n", ""},
		{"def f(a: int = 1, **kw: str): pass\n", ""},
		{"def f(a=1, *, b, **kw: str): pass\n", ""},
		{"def f(*args: int, **kw: str): pass\n", ""},
		{"f = lambda a, /, b, *c, d, **e: 0\n", ""},
		{"f(a, *b, c=1, *d, **e, f=2)\n", ""},
		{"f(x for x in y)\n", ""},
		{"[x for x, y in z if y]\n", ""},
		{"(x := 1)\n", ""},
		{"del a.b, c[0], (d), [e, (f, g)]\n", ""},
		{"with a as (b, c[0]), d as [*e, f.g]: pass\n", ""},
		{"(x): int = 1\n", ""},
		{"a.b: int\n", ""},
		{"a += 1\n", ""},
		{"from x import (a, b,)\n", ""},
		{"from . import a as b, c\n", ""},
		{"raise E(\"msg\") from None\n", ""},
		{"if x:\n\tif y:\n\t\tpass\n\telse:\n\t\tpass\n", ""},
		{"if x:\n    y = (1,\n\t2)\n    z = '''a\n\tb'''\n  # comment\n    w = 1\n", ""},
		{"x = 1 + \\\n\t2\n", ""},
		{deepOK + "\n", ""},
		{indentedOK.String(), ""},
		{"\xef\xbb\xbf# coding: utf-8\ndef f(): pass\n", ""},
		{"# -*- coding: latin-1 -*-\ndef f(): pass\n", ""},
		{"# coding=utf8\nx = '\xc3\xa9'\n", ""},
		{"x = 1  # coding: foobar after code on line 1\n", ""},
		{"x = 1\n# coding: foobar on line 2 after code\n", ""},
	}
	covered := map[string]bool{}
	for _, c := range cases {
		root := parseSource(t, c.source)
		if got := rejection(root, []byte(c.source)); got != c.want {
			t.Errorf("%q: rejection %q, want %q", c.source, got, c.want)
		}
		covered[c.want] = true
	}
	for _, form := range rejectForms {
		if !covered[form] {
			t.Errorf("rejection form %s has no case", form)
		}
	}
}
