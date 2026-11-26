package projectsuggest

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/parse"
)

// define is a build-time constant read from a literal define entry (row
// RowDefineEnv): at build time the bundler replaces name with the value
// of the environment variable env. Calls written with the constant record
// it as their base expression, so an env file address of env in the same
// folder is evidence for calls through name too.
type define struct {
	job      scriptJob
	name     string
	env      string
	evidence Evidence
}

var (
	// constantName is a plain identifier, the usual define key
	// (`__API_URL__`).
	constantName = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	// envMember is a define key or value naming one environment variable:
	// `process.env.X` or `import.meta.env.X`.
	envMember = regexp.MustCompile(`^(?:process\.env|import\.meta\.env)\.([A-Za-z_][A-Za-z0-9_]*)$`)
	// processEnv is the only variable a JSON.stringify value may read: the
	// config runs in Node, where import.meta.env does not exist.
	processEnv = regexp.MustCompile(`^process\.env\.([A-Za-z_][A-Za-z0-9_]*)$`)
)

// readWebpackDefines reads row RowDefineEnv and RowDefineURL from every
// `new DefinePlugin({…})` or `new webpack.DefinePlugin({…})` in a webpack
// config. The plugin's argument must be one literal object; where the
// plugin is created (inside a config function, a condition) does not
// change what an entry says, so the rest of the file may be computed.
func (s *state) readWebpackDefines(j scriptJob, root *parse.Node) {
	loads := loadsDotenv(root)
	walk(root, func(n *parse.Node) {
		if n.Kind != "new_expression" {
			return
		}
		ctor := n.Fields["constructor"]
		if ctor == nil || (ctor.Text != "DefinePlugin" && !strings.HasSuffix(ctor.Text, ".DefinePlugin")) {
			return
		}
		args := n.Fields["arguments"]
		var list []*parse.Node
		if args != nil {
			list = nonComments(args.Children)
		}
		if len(list) != 1 || list[0].Kind != "object" {
			s.unreadAt(j.member, j.path, int(n.Row)+1, "DefinePlugin argument is not one literal object")
			return
		}
		s.defineObject(j, list[0], "DefinePlugin", loads)
	})
}

// loadsDotenv reports whether a config loads env files into process.env
// with dotenv (`require('dotenv')`, `require('dotenv/config')`, or an
// import of either). Neither webpack nor Vite does that for the config by
// itself, so without it an env file is not tied to process.env there.
func loadsDotenv(root *parse.Node) bool {
	found := false
	walk(root, func(n *parse.Node) {
		var source *parse.Node
		switch n.Kind {
		case "import_statement":
			source = n.Fields["source"]
		case "call_expression":
			if f := n.Fields["function"]; f != nil && f.Text == "require" {
				if args := n.Fields["arguments"]; args != nil && len(nonComments(args.Children)) == 1 {
					source = nonComments(args.Children)[0]
				}
			}
		}
		if v, ok := literalString(source); ok && (v == "dotenv" || v == "dotenv/config") {
			found = true
		}
	})
	return found
}

// readViteDefines reads `define` of the literal exported Vite config (the
// same literal forms as RowViteProxy).
func (s *state) readViteDefines(j scriptJob, root *parse.Node) {
	obj, _ := exportedObject(root, true)
	if obj == nil {
		// Already listed as computed by the proxy reader.
		return
	}
	def, key, _ := property(obj, "define")
	if def == nil {
		return
	}
	if def.Kind != "object" {
		s.unreadAt(j.member, j.path, int(key.Row)+1, "define is computed in code")
		return
	}
	s.defineObject(j, def, "define", loadsDotenv(root))
}

// defineObject reads the entries of one literal define object. Entries
// whose value neither reads an environment variable nor holds a URL are
// not evidence and are skipped silently (`__DEV__: true`).
func (s *state) defineObject(j scriptJob, obj *parse.Node, what string, loadsEnv bool) {
	for _, c := range obj.Children {
		line := int(c.Row) + 1
		switch c.Kind {
		case "comment":
			continue
		case "pair":
		default:
			if strings.Contains(c.Text, "process.env") || strings.Contains(c.Text, "://") {
				s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s entry is not a literal key and value", what))
			}
			continue
		}
		value := c.Fields["value"]
		if value == nil || !(strings.Contains(value.Text, "process.env") || strings.Contains(value.Text, "import.meta.env") || strings.Contains(value.Text, "://")) {
			continue
		}
		key, ok := literalKey(c.Fields["key"])
		base := ""
		switch {
		case !ok:
		case constantName.MatchString(key):
			base = key
		case envMember.MatchString(key):
			base = envMember.FindStringSubmatch(key)[1]
		}
		if base == "" {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s key is computed or not a constant name", what))
			continue
		}
		env, url, reason := defineValue(value)
		switch {
		case reason != "":
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s %s: %s", what, key, reason))
		case env != "" && !loadsEnv:
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s %s reads process.env.%s, but the config loads no env file (no dotenv), so no env file is tied to it", what, key, env))
		case env != "":
			s.defines = append(s.defines, define{job: j, name: base, env: env,
				evidence: s.evidence(j.member, RowDefineEnv, j.path, line, key, "process.env."+env)})
		default:
			a, problem, isHTTP := readURL(url)
			if !isHTTP {
				continue
			}
			if problem != "" {
				s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s %s: %s", what, key, problem))
				continue
			}
			a.consumer, a.scope, a.base = j.member, scopeOf(j.member, j.path), base
			a.evidence = s.evidence(j.member, RowDefineURL, j.path, line, key, url)
			s.addresses = append(s.addresses, a)
		}
	}
}

// defineValue reads a define value: `JSON.stringify(process.env.X)` (the
// constant is variable X), `JSON.stringify('URL')` or the JSON text
// `'"URL"'` (the constant is that string). Anything else (a fallback, a
// variable, a template, raw code) is computed.
func defineValue(v *parse.Node) (env, url, reason string) {
	if v.Kind == "call_expression" && v.Fields["function"] != nil && v.Fields["function"].Text == "JSON.stringify" {
		var args []*parse.Node
		if a := v.Fields["arguments"]; a != nil {
			args = nonComments(a.Children)
		}
		if len(args) != 1 {
			return "", "", "JSON.stringify of more than one value"
		}
		if m := processEnv.FindStringSubmatch(args[0].Text); m != nil && args[0].Kind == "member_expression" {
			return m[1], "", ""
		}
		if s, ok := literalString(args[0]); ok {
			return "", s, ""
		}
		return "", "", "value is computed in code (only process.env.NAME or a literal string is read)"
	}
	if s, ok := literalString(v); ok {
		var text string
		if json.Unmarshal([]byte(s), &text) == nil && strings.HasPrefix(s, `"`) {
			return "", text, ""
		}
		return "", "", "value is code, not a JSON string (only JSON.stringify(...) or '\"URL\"' is read)"
	}
	return "", "", "value is computed in code (only process.env.NAME or a literal string is read)"
}

// walk calls fn for n and every node below it.
func walk(n *parse.Node, fn func(*parse.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walk(c, fn)
	}
}

// aliases returns the define constants of a's member whose config lies in
// the folder of a's env file and which read a's key (rows RowDefineEnv
// and RowEnvURL). dotenv reads env files from the folder the build runs
// in, normally the config's, so a constant defined elsewhere is not tied
// to this file.
func (s *state) aliases(a address) []define {
	if a.evidence.Row != RowEnvURL {
		return nil
	}
	var out []define
	for _, d := range s.defines {
		if d.job.member == a.consumer && d.env == a.base && filepath.Dir(d.job.path) == a.dir {
			out = append(out, d)
		}
	}
	return out
}
