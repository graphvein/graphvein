package golang

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// wrapperHead is the file header of the wrapper proof cases.
const wrapperHead = "package p\n\nimport (\n\t\"context\"\n\t\"net/http\"\n\n\t\"example.com/m/req\"\n)\n\n"

// wrapperBody wraps a handler literal's statements in a wrapper W.
func wrapperBody(stmts string) string {
	return wrapperHead + "func W(next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n" + stmts + "\n\t})\n}\n"
}

// TestGoWrapperProof checks the local part of the path-preservation proof
// (rows RW-wrapper and RW-D): what may run before a serving call only
// reads the request or passes it to a named function; a branch that ends
// in a terminating statement, uses after the serving call and a deferred
// literal's body do not count; anything else refuses the wrapper.
func TestGoWrapperProof(t *testing.T) {
	cases := []struct {
		name, src string
		ok        bool
		context   bool
		passes    string
	}{
		{"identity", wrapperHead + "func W(h http.Handler) http.Handler { return h }\n", true, false, ""},
		{"reads", wrapperBody("\t\tif r.Method == http.MethodGet && r.URL.Path != \"\" && r.Header.Get(\"x\") == \"\" {\n\t\t\tw.WriteHeader(204)\n\t\t}\n\t\t_ = r.URL.Query().Get(\"q\") + r.FormValue(\"f\")\n\t\tnext.ServeHTTP(w, r)"), true, false, ""},
		{"terminating branch", wrapperBody("\t\tif r.Method == \"OPTIONS\" {\n\t\t\tr.URL.Path = \"/\"\n\t\t\tpanic(\"x\")\n\t\t}\n\t\tswitch {\n\t\tcase r.Header.Get(\"x\") == \"\":\n\t\t\tr.Method = \"GET\"\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), true, false, ""},
		{"after the serving call", wrapperBody("\t\tnext.ServeHTTP(w, r)\n\t\tr.URL.Path = \"/after\""), true, false, ""},
		{"context and passes", wrapperBody("\t\tctx := context.WithValue(r.Context(), \"k\", req.IP(r))\n\t\tif !allowed(w, r) {\n\t\t\treturn\n\t\t}\n\t\tr = r.WithContext(ctx)\n\t\tnext.ServeHTTP(w, r)"), true, true, "req.IP/0 allowed/1"},
		{"deferred literal", wrapperBody("\t\tdefer func() { r.URL.Path = \"/\" }()\n\t\tnext.ServeHTTP(w, r.WithContext(r.Context()))"), true, true, ""},
		{"write before", wrapperBody("\t\tr.URL.Path = \"/x\"\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"header map write", wrapperBody("\t\tr.Header[\"X\"] = nil\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"alias", wrapperBody("\t\tr2 := r\n\t\t_ = r2\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"url as a value", wrapperBody("\t\tfix(r.URL)\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"pass through a field", wrapperBody("\t\tm.store.Log(r)\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"local callee", wrapperBody("\t\tfix := func(*http.Request) {}\n\t\tfix(r)\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"literal called before", wrapperBody("\t\tfix := func() { r.URL.Path = \"/\" }\n\t\tfix()\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"break out of a returning branch", wrapperBody("\t\tswitch {\n\t\tcase r.Host == \"\":\n\t\t\tr.URL.Path = \"/\"\n\t\t\tif r.Method == \"\" {\n\t\t\t\tbreak\n\t\t\t}\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"loop", wrapperBody("\t\tfor i := 0; i < 2; i++ {\n\t\t\tnext.ServeHTTP(w, r)\n\t\t\tr.URL.Path = \"/again\"\n\t\t}"), false, false, ""},
		{"goroutine", wrapperBody("\t\tgo func() { r.URL.Path = \"/\" }()\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"label", wrapperBody("\tL:\n\t\tnext.ServeHTTP(w, r)\n\t\tgoto L"), false, false, ""},
		{"serving in a literal", wrapperBody("\t\tgo next.ServeHTTP(w, r)"), false, false, ""},
		{"handler escapes", wrapperBody("\t\tkeep(next)\n\t\tnext.ServeHTTP(w, r)"), false, false, ""},
		{"another request", wrapperBody("\t\tnext.ServeHTTP(w, r.Clone(r.Context()))"), false, false, ""},
		{"no serving call", wrapperBody("\t\tw.WriteHeader(404)"), false, false, ""},
		{"named handler", wrapperHead + "func W(next http.Handler) http.Handler { return http.HandlerFunc(serve) }\n", false, false, ""},
		{"handler returned by a closure", wrapperHead + "func W(next http.Handler) http.Handler {\n\tf := func() http.Handler { return next }\n\t_ = f\n\treturn next\n}\n", false, false, ""},
		{"shadowed http", wrapperHead + "func W(next http.Handler) http.Handler {\n\thttp := 1\n\t_ = http\n\treturn next\n}\n", false, false, ""},
	}
	for _, c := range cases {
		f := parseGo(t, "w.go", c.src)
		ws := f.Go.Wrappers
		if (len(ws) == 1) != c.ok {
			t.Errorf("%s: wrappers %+v, want proven %v", c.name, ws, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		var passes []string
		for _, p := range ws[0].Passes {
			name := p.Callee.Name
			if p.Callee.Object != nil {
				name = p.Callee.Object.Name + "." + name
			}
			passes = append(passes, name+"/"+string(rune('0'+p.Arg)))
		}
		if ws[0].Context != c.context || strings.Join(passes, " ") != c.passes {
			t.Errorf("%s: context %v passes %q, want %v %q", c.name, ws[0].Context, passes, c.context, c.passes)
		}
	}
}

// TestGoRequestHelpers checks row RQ-request: a function or method whose
// request parameter is only read or passed on is recorded with its passes;
// a write, an alias, a return or a store of the request refuses it.
func TestGoRequestHelpers(t *testing.T) {
	f := parseGo(t, "h.go", wrapperHead+
		"func IP(r *http.Request) string { return r.RemoteAddr + req.Real(r) }\n"+
		"func (s *S) check(w http.ResponseWriter, r *http.Request) bool { return s.ok(r) && IP(r) != \"\" }\n"+
		"func write(r *http.Request) { r.Method = \"GET\" }\n"+
		"func ret(r *http.Request) *http.Request { return r }\n"+
		"func store(r *http.Request) { last = holder{r} }\n"+
		"func two(a, b *http.Request) {}\n")
	var got []string
	for _, r := range f.Go.Requests {
		s := r.Receiver + "." + r.Name + ":" + string(rune('0'+r.Param))
		for _, p := range r.Passes {
			s += " " + p.Callee.Name
			if p.Method {
				s += "(method)"
			}
		}
		got = append(got, s)
	}
	want := ".IP:0 Real|S.check:1 ok(method) IP"
	if strings.Join(got, "|") != want {
		t.Errorf("requests %q, want %q", strings.Join(got, "|"), want)
	}
}

// forwardFile wraps a handler literal's statements in a ServeMux
// registration: m.HandleFunc("/", func(w, r) {...}) with root captured.
func forwardFile(stmts string) string {
	return "package p\n\nimport (\n\t\"net/http\"\n\t\"strings\"\n\n\t\"example.com/m/req\"\n)\n\n" +
		"func f(root http.Handler) {\n\tm := http.NewServeMux()\n\tm.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) {\n" + stmts + "\n\t})\n\t_ = strings.TrimSpace\n\t_ = req.IP\n}\n"
}

// TestGoForwardProof checks the forwarding proof of a function literal
// (rows RE-forward and RE-forward-D, T023): the literal serves the
// request it receives, unchanged, through one captured name; the string
// literals r.URL.Path is switched on or compared with are its shadows;
// every other shape is refused.
func TestGoForwardProof(t *testing.T) {
	cases := []struct {
		name, stmts string
		ok          bool
		shadows     string
		passes      string
	}{
		{"plain", "\t\troot.ServeHTTP(w, r)", true, "", ""},
		{"switch default", "\t\tswitch r.URL.Path {\n\t\tcase \"/debug/vars\":\n\t\t\tw.WriteHeader(404)\n\t\t\treturn\n\t\tcase \"/embed/chat/\", \"/embed/chat\":\n\t\t\thttp.Redirect(w, r, \"/x\", 307)\n\t\tdefault:\n\t\t\troot.ServeHTTP(w, r)\n\t\t}", true, "/debug/vars /embed/chat /embed/chat/", ""},
		{"if equal", "\t\tif r.URL.Path == `/health` {\n\t\t\treturn\n\t\t}\n\t\tif !req.Allowed(r) {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", true, "/health", "req.Allowed/0"},
		{"served in a path case", "\t\tswitch r.URL.Path {\n\t\tcase \"/a\":\n\t\t\troot.ServeHTTP(w, r)\n\t\t}", false, "", ""},
		{"served in a path branch", "\t\tif r.URL.Path == \"/a\" {\n\t\t\troot.ServeHTTP(w, r)\n\t\t}", false, "", ""},
		{"not equal", "\t\tif r.URL.Path != \"/a\" {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"negated", "\t\tif !(r.URL.Path == \"/a\") {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"prefix", "\t\tif strings.HasPrefix(r.URL.Path, \"/a\") {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"non-literal case", "\t\tswitch r.URL.Path {\n\t\tcase prefix:\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"request URI", "\t\tif r.RequestURI == \"/a\" {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"with context", "\t\troot.ServeHTTP(w, r.WithContext(r.Context()))", false, "", ""},
		{"rebound", "\t\tr = r.WithContext(r.Context())\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"write before", "\t\tr.URL.Path = \"/x\"\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"target assigned", "\t\troot = nil\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"target declared", "\t\troot := pick()\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"two targets", "\t\troot.ServeHTTP(w, r)\n\t\tother.ServeHTTP(w, r)", false, "", ""},
		{"target used otherwise", "\t\tkeep(root)\n\t\troot.ServeHTTP(w, r)", false, "", ""},
		{"serving in a goroutine", "\t\tgo root.ServeHTTP(w, r)", false, "", ""},
		{"no serving call", "\t\tw.WriteHeader(204)", false, "", ""},
		{"host answered", "\t\tif r.Host == \"example.org\" {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)", false, "", ""},
	}
	for _, c := range cases {
		f := parseGo(t, "f.go", forwardFile(c.stmts))
		var fwd *facts.GoForward
		for _, fn := range f.Go.Routes.Funcs {
			for _, call := range fn.Calls {
				for _, a := range call.Call.Args {
					if a.Kind == facts.GoFuncLit && a.Forward != nil {
						fwd = a.Forward
					}
				}
			}
		}
		if (fwd != nil) != c.ok {
			t.Errorf("%s: forward %+v, want proven %v", c.name, fwd, c.ok)
			continue
		}
		if fwd == nil {
			continue
		}
		var passes []string
		for _, p := range fwd.Passes {
			name := p.Callee.Name
			if p.Callee.Object != nil {
				name = p.Callee.Object.Name + "." + name
			}
			passes = append(passes, name+"/"+string(rune('0'+p.Arg)))
		}
		if fwd.Name != "root" || strings.Join(fwd.Shadows, " ") != c.shadows || strings.Join(passes, " ") != c.passes {
			t.Errorf("%s: forward %+v (passes %q), want root, shadows %q, passes %q", c.name, fwd, passes, c.shadows, c.passes)
		}
	}
}

// TestGoWrapperDecisions checks the deciding statements of a wrapper:
// a branch that ends or serves chosen by r.URL
// refuses the proof; one chosen by r.Method == M records M; a request
// pass in a deciding condition is a guard; any other deciding read of
// r.Method refuses the proof. A forwarding closure records the same.
func TestGoWrapperDecisions(t *testing.T) {
	cases := []struct {
		name, src       string
		ok              bool
		methods, guards string
	}{
		{"path answered", wrapperBody("\t\tif r.URL.Path == \"/health\" {\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"prefix served elsewhere", wrapperBody("\t\tif strings.HasPrefix(r.URL.Path, \"/static/\") {\n\t\t\tfs.ServeHTTP(w, r)\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"request uri", wrapperBody("\t\tswitch r.RequestURI {\n\t\tcase \"/x\":\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"path read without deciding", wrapperBody("\t\tif r.URL.Path == \"/upload\" {\n\t\t\tw.Header().Set(\"x\", \"y\")\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), true, "", ""},
		{"preflight", wrapperBody("\t\tif r.Method == http.MethodOptions {\n\t\t\tw.WriteHeader(204)\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), true, "OPTIONS", ""},
		{"method switch", wrapperBody("\t\tswitch r.Method {\n\t\tcase \"TRACE\", http.MethodConnect:\n\t\t\treturn\n\t\tcase \"GET\":\n\t\t\tw.Header().Set(\"x\", \"y\")\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), true, "CONNECT TRACE", ""},
		{"method not equal", wrapperBody("\t\tif r.Method != http.MethodGet {\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"served for one method", wrapperBody("\t\tif r.Method == \"GET\" {\n\t\t\tnext.ServeHTTP(w, r)\n\t\t}"), false, "", ""},
		{"default ends", wrapperBody("\t\tswitch r.Method {\n\t\tcase \"GET\":\n\t\tdefault:\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"host answered", wrapperBody("\t\tif r.Host == \"example.org\" {\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"url host answered", wrapperBody("\t\tif r.URL.Host != \"\" {\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), false, "", ""},
		{"guard", wrapperBody("\t\tif !req.Allowed(r) {\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)"), true, "", "req.Allowed/0"},
	}
	for _, c := range cases {
		src := strings.Replace(c.src, "\"context\"\n", "\"context\"\n\t\"strings\"\n", 1)
		f := parseGo(t, "w.go", src)
		ws := f.Go.Wrappers
		if (len(ws) == 1) != c.ok {
			t.Errorf("%s: wrappers %+v, want proven %v", c.name, ws, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		var guards []string
		for _, g := range ws[0].Guards {
			guards = append(guards, passText(g))
		}
		if got := strings.Join(ws[0].Methods, " "); got != c.methods {
			t.Errorf("%s: methods %q, want %q", c.name, got, c.methods)
		}
		if got := strings.Join(guards, " "); got != c.guards {
			t.Errorf("%s: guards %q, want %q", c.name, got, c.guards)
		}
	}
	f := parseGo(t, "f.go", forwardFile("\t\tif r.Method == \"OPTIONS\" {\n\t\t\treturn\n\t\t}\n\t\tif req.Static(r) {\n\t\t\treturn\n\t\t}\n\t\troot.ServeHTTP(w, r)"))
	var fwd *facts.GoForward
	for _, fn := range f.Go.Routes.Funcs {
		for _, call := range fn.Calls {
			for _, a := range call.Call.Args {
				if a.Kind == facts.GoFuncLit && a.Forward != nil {
					fwd = a.Forward
				}
			}
		}
	}
	if fwd == nil || strings.Join(fwd.Methods, " ") != "OPTIONS" || len(fwd.Guards) != 1 || passText(fwd.Guards[0]) != "req.Static/0" {
		t.Errorf("forward %+v", fwd)
	}
}

// passText renders a request pass as name/arg.
func passText(p facts.GoRequestPass) string {
	name := p.Callee.Name
	if p.Callee.Object != nil {
		name = p.Callee.Object.Name + "." + name
	}
	return name + "/" + string(rune('0'+p.Arg))
}
