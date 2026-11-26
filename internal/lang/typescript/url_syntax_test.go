package typescript

import "testing"

func TestJavaScriptStringEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`'\x2fapi'`, "/api"}, {`"\u002fapi"`, "/api"}, {"`a\\\nb`", "ab"}, {`'\ud83d\ude00'`, "😀"}, {`'\u{1f600}'`, "😀"}, {`'\v'`, "\v"}, {`'\/'`, "/"}} {
		got, ok := jsString(tc.raw)
		if !ok || got != tc.want {
			t.Errorf("%s: %q %t", tc.raw, got, ok)
		}
	}
	for _, raw := range []string{`'\ud800'`, `'\uZZZZ'`, `'\08'`, `'\1'`} {
		if _, ok := jsString(raw); ok {
			t.Errorf("accepted unsupported escape %s", raw)
		}
	}
}

func TestNumericPropertyKeysUseECMAScriptSpelling(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{"0x10", "16"}, {"1e21", "1e+21"}, {"1e-7", "1e-7"}, {"1.5", "1.5"}, {"1_000", "1000"}} {
		got, ok := numericPropertyKey(tc.raw)
		if !ok || got != tc.want {
			t.Errorf("%s: %s %t", tc.raw, got, ok)
		}
	}
}
