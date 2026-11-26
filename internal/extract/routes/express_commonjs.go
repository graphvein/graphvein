package routes

import (
	"path"
	"strings"
)

// CommonJS modules (T036, rows CJ-U1..CJ-U4 and CJ-E1..CJ-E4 of
// docs/extract/routes.md). A `require` of a relative module resolves to the
// file's CommonJS exports the way an ESM import resolves to its exports, so
// a router a module exports with `module.exports = router` composes through
// `app.use("/p", require("./routes"))` exactly as through
// `import routes from "./routes"`.

// requiredFile resolves the specifier of `require("spec")` in file to a
// repository module. local reports a relative specifier: it never names a
// package, so one that does not resolve gives target "" (an unresolved
// mount, never a factory).
//
// Only a JavaScript requirer (`.js`, `.cjs`) is read, and it resolves as
// Node's CommonJS loader does (LOAD_AS_FILE, then LOAD_AS_DIRECTORY): the
// path itself, then with `.js`, `.json`, `.node` appended, then the
// directory's `index` with the same extensions; a directory with its own
// package.json (a "main" field) is refused. The first candidate in the
// inventory wins, and only a `.js` or `.cjs` module has CommonJS exports
// to read; an earlier candidate on disk that was not scanned gives no
// target (nodeRequire). A compiler's options do not apply: `require` is resolved at run
// time, never as an import. A TypeScript requirer runs as compiled output
// whose layout the cached syntax does not show, and an ES module has no
// `require`: both give no target.
func (r *resolver) requiredFile(file, spec string) (target string, local bool) {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && spec != "." && spec != ".." {
		return "", false
	}
	if ext := path.Ext(file); ext != ".js" && ext != ".cjs" {
		return "", true
	}
	return r.nodeRequire(file, spec), true
}

// nodeRequire is Node's CommonJS resolution of a relative specifier from a
// JavaScript file over the scanned inventory (see requiredFile). A
// candidate that exists on disk but was not scanned (an ignored, skipped
// or dropped entry, as the scanner lists them: typescript.ModuleResolver
// Unread) and comes before the first scanned one is the module Node
// loads, whose code Graphvein never read: the require gives no target and
// the unread entry is recorded in r.unscanned for the mount's
// route_mount_unresolved diagnostic.
func (r *resolver) nodeRequire(file, spec string) string {
	base := path.Join(path.Dir(file), spec)
	if strings.HasPrefix(base, "../") || base == ".." {
		return ""
	}
	exists := func(p string) bool {
		_, ok := r.files[p]
		return ok
	}
	// try reports whether candidate c decides the outcome: a scanned file
	// (target is c when Node reads it as CommonJS JavaScript) or an
	// unread entry (no target).
	try := func(c string) (target string, decided bool) {
		if exists(c) {
			if ext := path.Ext(c); ext == ".js" || ext == ".cjs" {
				return c, true
			}
			return "", true
		}
		if entry, unread := r.modules.Unread(c); unread {
			r.unscanned = entry
			return "", true
		}
		return "", false
	}
	for _, c := range []string{base, base + ".js", base + ".json", base + ".node"} {
		if target, decided := try(c); decided {
			return target
		}
	}
	// A directory with its own package.json loads its "main": refused,
	// whether the file was scanned or not.
	if _, decided := try(path.Join(base, "package.json")); decided {
		return ""
	}
	for _, c := range []string{path.Join(base, "index.js"), path.Join(base, "index.json"), path.Join(base, "index.node")} {
		if target, decided := try(c); decided {
			return target
		}
	}
	return ""
}

// commonJSModule is the value `require` returns for target: the module
// value of `module.exports = E` (CJ-E1), or the exports object whose
// members CJ-E2 and CJ-E3 read. A file with unread exports, or without
// CommonJS exports (an ES module), gives no value.
func (r *resolver) commonJSModule(target string, depth int, seen map[string]bool) value {
	data := r.files[target].Routes
	if target == "" || data == nil || data.CommonJS == nil || data.CommonJS.Unread {
		return value{}
	}
	exports := data.CommonJS
	if exports.Module != nil && exports.Module.Kind != "object" {
		return r.resolve(target, 0, *exports.Module, depth, seen)
	}
	return value{kind: "commonjs", file: target}
}

// commonJSMember is member name of target's exports object: a property of
// the object literal `module.exports = { name, other: value }` without a
// spread element (CJ-E2), or the value of `exports.name = E` (CJ-E3).
func (r *resolver) commonJSMember(target, name string, depth int, seen map[string]bool) value {
	exports := r.files[target].Routes.CommonJS
	if exports.Module != nil {
		if exports.Module.Spread {
			return value{}
		}
		if property, ok := exports.Module.Properties[name]; ok {
			return r.resolve(target, 0, property, depth, seen)
		}
		return value{}
	}
	if member, ok := exports.Members[name]; ok {
		return r.resolve(target, 0, member, depth, seen)
	}
	return value{}
}
