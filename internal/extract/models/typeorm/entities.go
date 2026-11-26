package typeorm

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads which entity classes a group reaches (rows TO-K1 and
// TO-K4): the `entities` option's class references and the statically
// resolvable glob subset, and with `autoLoadEntities: true` the forFeature
// lists registered for the group's data source name; and the migrations
// option (TO-U1).

// loaderExtensions are the files TypeORM's directory loader imports
// (util/DirectoryExportedClassesLoader.js:14-52 at 0.3.31, :16-39 at
// 1.1.3); `.d.ts` files are skipped.
var loaderExtensions = []string{".js", ".mjs", ".cjs", ".ts", ".mts", ".cts"}

func loaderFile(f string) bool {
	if strings.HasSuffix(f, ".d.ts") {
		return false
	}
	for _, e := range loaderExtensions {
		if strings.HasSuffix(f, e) {
			return true
		}
	}
	return false
}

// reach fills the group's reached entities.
func (d *discovery) reach(g *Group) {
	p := g.Package
	o := g.Options[0]
	seen := map[*Entity]bool{}
	add := func(e *Entity) {
		if !seen[e] {
			seen[e] = true
			g.Entities = append(g.Entities, e)
		}
	}
	nest := g.Form == "forRoot" || g.Form == "forRootAsync"
	v, has := o.keys["entities"]
	if has && !isUndefined(v.Expr) {
		list, at, why := d.entityList(v.File, v.Expr, nest && g.AutoLoad, p)
		g.EntitiesAt = at
		switch {
		case why != "":
			g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities option is not read (%s)", v.File, v.Expr.Span.StartLine, why))
		default:
			d.elements(g, v.File, list, add)
		}
	}
	if !nest || !g.AutoLoad {
		return
	}
	d.in.tag("TO-K4/`autoLoadEntities: true`")
	if !g.NameKnown {
		g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the data source name is not a literal, so the forFeature calls are not attributed", g.File, g.Span.StartLine))
		return
	}
	// Only the forFeature calls of files the root's application loads
	// through static imports register for this root (Revision 4 A4,
	// appReach): a call in a file this root does not reach is not counted, and leaves the list
	// incomplete when the root's import graph cannot be followed to its end
	// or no root of the package reaches that file (a dynamic import may
	// load it).
	reach := d.appReach(p, g.File)
	byAny := map[string]bool{}
	for _, other := range p.Groups {
		if other.Form == "forRoot" || other.Form == "forRootAsync" {
			for f := range d.appReach(p, other.File).files {
				byAny[f] = true
			}
		}
	}
	for _, ft := range p.Features {
		if ft.NameKnown && ft.Name != g.Name {
			continue
		}
		if !reach.files[ft.File] {
			switch {
			case len(reach.open) > 0:
				g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: a TypeOrmModule.forFeature call in a file the root's static imports do not reach, and the import graph is open (%s)", ft.File, ft.Span.StartLine, strings.Join(reach.open, "; ")))
			case !byAny[ft.File]:
				g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: a TypeOrmModule.forFeature call in a file no root module's static imports reach (a dynamic import may load it)", ft.File, ft.Span.StartLine))
			}
			continue
		}
		if !ft.NameKnown {
			g.Incomplete = append(g.Incomplete, ft.Unread...)
			continue
		}
		g.Incomplete = append(g.Incomplete, ft.Unread...)
		for _, e := range ft.Entities {
			add(e)
		}
	}
}

// entityList returns the elements of an entities option: an array
// literal, or an identifier bound to a module-level const array literal.
// With autoLoadEntities, @nestjs/typeorm 11.x replaces a non-array option
// by the forFeature list and 12.x appends that list to an object map's
// values (`typeorm-core.module.js:176-179` at 11.0.0, `:182-186` at
// 12.0.3); a string option is not read on any version.
func (d *discovery) entityList(file string, e *typescript.ModelExpr, autoLoad bool, p *Package) (list []optionValue, at, why string) {
	if e.Kind == typescript.ModelIdentifier {
		b := d.local(file, e.Name, 0)
		v, why := d.constValue(b)
		if why != "" {
			return nil, "", e.Name + ": " + why
		}
		if v.Kind != typescript.ModelArray {
			return nil, "", e.Name + " is not bound to an array literal"
		}
		file, e = b.File, v
	}
	at = file + ":" + itoa(e.Span.StartLine)
	var out []optionValue
	switch {
	case e.Kind == typescript.ModelArray:
		for i := range e.Elements {
			out = append(out, optionValue{Expr: &e.Elements[i], File: file})
		}
	case autoLoad && e.Kind == typescript.ModelObject && p.NestMajor() >= 0 && p.NestMajor() < 12:
		// Replaced by the forFeature list.
	case autoLoad && e.Kind == typescript.ModelObject && p.NestMajor() >= 12:
		for i := range e.Entries {
			en := &e.Entries[i]
			if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
				return nil, "", fmt.Sprintf("an entities map entry at line %d is not read", en.Span.StartLine)
			}
			out = append(out, optionValue{Expr: &en.Value, File: file})
		}
	default:
		return nil, "", "it is not an array literal or a const bound to one"
	}
	return out, at, ""
}

// elements reads the entities elements: identifiers of `@Entity` classes
// (class references) and the glob subset; any other element (a spread, an
// EntitySchema, a call) makes the table list incomplete.
func (d *discovery) elements(g *Group, file string, list []optionValue, add func(*Entity)) {
	p := g.Package
	type globEl struct {
		el      optionValue
		pattern string
		matched []string
	}
	var globs []*globEl
	for _, el := range list {
		e := el.Expr
		line := e.Span.StartLine
		if e.Kind == typescript.ModelIdentifier {
			en, why := d.entityOf(p, el.File, e)
			if en == nil {
				g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities element %s is not read (%s)", el.File, line, e.Name, why))
				continue
			}
			d.in.tag("TO-K1/class references")
			if en.Typeorm && en.Form == "Entity" {
				add(en)
			} else if !en.Typeorm {
				g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities element %s has an @Entity decorator that is not typeorm's", el.File, line, e.Name))
			}
			continue
		}
		pattern, why := d.globPattern(p, el.File, e)
		if why != "" {
			g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities element is not read (%s)", el.File, line, why))
			continue
		}
		matched, why := d.globFiles(pattern)
		if why != "" {
			g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities glob %q is not read (%s)", el.File, line, pattern, why))
			continue
		}
		d.in.tag("TO-K1/the glob subset")
		globs = append(globs, &globEl{el: el, pattern: pattern, matched: matched})
	}
	for _, gl := range globs {
		if len(gl.matched) > 0 {
			continue
		}
		// Open point O18: a pattern that matches no file and differs from
		// a pattern that matches files only by a .js/.ts extension adds
		// nothing (a compiled-output sibling).
		sibling := false
		for _, other := range globs {
			if len(other.matched) > 0 && extensionSibling(gl.pattern, other.pattern) {
				sibling = true
			}
		}
		if !sibling {
			g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities glob %q matches no source file (build output or a missing directory)", gl.el.File, gl.el.Expr.Span.StartLine, gl.pattern))
		}
	}
	for _, gl := range globs {
		for _, f := range gl.matched {
			if d.owner(f) != p.Dir {
				if d.hasEntityClass(f) {
					g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: the entities glob %q reaches %s, an entity file of another package", gl.el.File, gl.el.Expr.Span.StartLine, gl.pattern, f))
				}
				continue
			}
			for _, en := range p.Entities {
				if en.File == f && en.Exported && en.Typeorm && en.Form == "Entity" {
					add(en)
				} else if en.File == f && en.Exported && !en.Typeorm {
					g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s: class %s has an @Entity decorator that is not typeorm's", f, en.Name))
				}
			}
		}
	}
}

// hasEntityClass reports a module-level class with an entity-named
// decorator in a file's payload.
func (d *discovery) hasEntityClass(f string) bool {
	m := d.in.Models[f]
	if m == nil {
		return false
	}
	for i := range m.Statements {
		s := &m.Statements[i]
		for j := range s.DecoratorList {
			c := &s.DecoratorList[j]
			if c.Kind == typescript.ModelCall {
				c = c.Callee
			}
			if (c.Kind == typescript.ModelIdentifier || c.Kind == typescript.ModelMember) && entityDecorators[c.Name] {
				return true
			}
		}
	}
	return false
}

// extensionSibling reports patterns equal but for a final .js/.ts.
func extensionSibling(a, b string) bool {
	trim := func(s string) string {
		for _, e := range []string{".js", ".ts"} {
			if strings.HasSuffix(s, e) {
				return strings.TrimSuffix(s, e)
			}
		}
		return s + "\x00"
	}
	return a != b && trim(a) == trim(b)
}

// globPattern resolves an entities or migrations element to a
// repository-relative pattern: a string literal (relative to the package
// directory, the working directory of TO-V5), `${__dirname}/...`,
// `__dirname + "..."`, or path.join / path.resolve (from `path`, as a
// namespace, a default import or the functions imported by name) of
// `__dirname` and string literals; `__dirname` is the directory of the
// file the expression is written in.
func (d *discovery) globPattern(p *Package, file string, e *typescript.ModelExpr) (string, string) {
	dirname := func(x *typescript.ModelExpr) bool {
		return x != nil && x.Kind == typescript.ModelIdentifier && x.Name == "__dirname"
	}
	here := path.Dir(file)
	var base string
	var parts []string
	switch {
	case e.Kind == typescript.ModelString:
		base, parts = p.Dir, []string{e.Value}
	case e.Kind == typescript.ModelTemplate && len(e.Substitutions) == 0:
		base, parts = p.Dir, []string{e.Strings[0]}
	case e.Kind == typescript.ModelTemplate && len(e.Substitutions) == 1 && e.Strings[0] == "" && dirname(&e.Substitutions[0]):
		base, parts = here, []string{"." + e.Strings[1]}
	case e.Kind == typescript.ModelBinary && e.Value == "+" && dirname(e.Left) && e.Right.Kind == typescript.ModelString:
		base, parts = here, []string{"." + e.Right.Value}
	case e.Kind == typescript.ModelCall:
		fn := d.expr(file, e.Callee)
		if fn.External != "path" && fn.External != "node:path" || fn.Name != "join" && fn.Name != "resolve" {
			return "", "a call other than path.join or path.resolve"
		}
		if e.Callee.Kind == typescript.ModelIdentifier {
			d.in.tag("TO-K1/`join(...)`/`resolve(...)` imported by name")
		}
		args := e.Args
		base = p.Dir
		fromDirname := len(args) > 0 && dirname(&args[0])
		if fromDirname {
			base, args = here, args[1:]
		}
		for i := range args {
			s, ok := stringLiteral(&args[i])
			if !ok {
				return "", "a path." + fn.Name + " argument is not a string literal"
			}
			// path.resolve restarts at an absolute segment; path.join
			// keeps one only as its first argument.
			if strings.HasPrefix(s, "/") && (fn.Name == "resolve" || i == 0 && !fromDirname) {
				return "", "an absolute path cannot be placed in the repository"
			}
			parts = append(parts, s)
		}
	default:
		return "", "it is not a class reference, a string or a path built from __dirname and string literals"
	}
	if base == p.Dir && len(parts) == 1 && strings.HasPrefix(parts[0], "/") {
		return "", "an absolute path cannot be placed in the repository"
	}
	for _, s := range parts {
		if strings.Contains(s, `\`) {
			return "", "a backslash in the pattern"
		}
	}
	joined := path.Join(append([]string{base}, parts...)...)
	if joined == ".." || strings.HasPrefix(joined, "../") || strings.HasPrefix(joined, "/") {
		return "", "the pattern is outside the repository"
	}
	return joined, ""
}

// globFiles returns the loader files (sorted) a repository-relative
// pattern matches: `*`, `**`, `?` and one level of `{a,b}`, with glob's
// default of not matching names that start with a dot; why is set for
// other glob syntax.
func (d *discovery) globFiles(pattern string) ([]string, string) {
	alternatives, why := expandBraces(pattern)
	if why != "" {
		return nil, why
	}
	seen := map[string]bool{}
	var out []string
	for _, alt := range alternatives {
		if strings.ContainsAny(alt, "[]{}") || strings.Contains(alt, "!(") || strings.Contains(alt, "+(") || strings.Contains(alt, "@(") || strings.Contains(alt, "*(") {
			return nil, "glob syntax outside the subset (`*`, `**`, `?`, one level of `{a,b}`)"
		}
		segs := strings.Split(alt, "/")
		for i, s := range segs {
			if s == ".." && i > 0 && hasMagic(strings.Join(segs[:i], "/")) {
				return nil, "`..` after a wildcard"
			}
		}
		for _, f := range d.files {
			if !seen[f] && loaderFile(f) && !underNodeModules(f) && matchSegments(segs, strings.Split(f, "/")) {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out, ""
}

// expandBraces expands one `{a,b,...}` group; nested or several groups
// are outside the subset.
func expandBraces(p string) ([]string, string) {
	i := strings.Index(p, "{")
	if i < 0 {
		if strings.Contains(p, "}") {
			return nil, "an unbalanced brace"
		}
		return []string{p}, ""
	}
	j := strings.Index(p[i:], "}")
	if j < 0 {
		return nil, "an unbalanced brace"
	}
	j += i
	inner := p[i+1 : j]
	rest := p[j+1:]
	if strings.ContainsAny(inner, "{") || strings.ContainsAny(rest, "{}") || !strings.Contains(inner, ",") {
		return nil, "glob syntax outside the subset (one level of `{a,b}` only)"
	}
	var out []string
	for _, alt := range strings.Split(inner, ",") {
		out = append(out, p[:i]+alt+rest)
	}
	return out, ""
}

func hasMagic(s string) bool { return strings.ContainsAny(s, "*?") }

// matchSegments matches path segments against pattern segments; a `**`
// segment matches zero or more segments that do not start with a dot.
func matchSegments(p, s []string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == "**" {
		if matchSegments(p[1:], s) {
			return true
		}
		return len(s) > 0 && !strings.HasPrefix(s[0], ".") && matchSegments(p, s[1:])
	}
	if len(s) == 0 || !matchSegment(p[0], s[0]) {
		return false
	}
	return matchSegments(p[1:], s[1:])
}

func matchSegment(p, s string) bool {
	if !hasMagic(p) {
		return p == s
	}
	if strings.HasPrefix(s, ".") && !strings.HasPrefix(p, ".") {
		return false
	}
	ok, err := path.Match(p, s)
	return err == nil && ok
}

// migrations reads the migrations option of every candidate (TO-U1): the
// literal patterns, the directory before each pattern's first wildcard and
// the script files it matches (a build-output pattern matches none and is
// listed as named only). The option is never read further.
func (d *discovery) migrations(g *Group) {
	seen := map[string]bool{}
	for _, o := range g.Options {
		v, ok := o.keys["migrations"]
		if !ok {
			continue
		}
		g.HasMigrations = true
		var list []optionValue
		var collect func(e *typescript.ModelExpr)
		collect = func(e *typescript.ModelExpr) {
			switch e.Kind {
			case typescript.ModelArray:
				for i := range e.Elements {
					list = append(list, optionValue{Expr: &e.Elements[i], File: v.File})
				}
			case typescript.ModelCond:
				collect(e.Then)
				collect(e.Else)
			default:
				list = append(list, optionValue{Expr: e, File: v.File})
			}
		}
		collect(v.Expr)
		for _, el := range list {
			m := Migration{File: el.File, Line: el.Expr.Span.StartLine}
			pattern, why := d.globPattern(g.Package, el.File, el.Expr)
			if why == "" {
				var files []string
				files, why = d.globFiles(pattern)
				m.Files = len(files)
			}
			m.Pattern, m.Why = pattern, why
			if why == "" {
				m.Dir = patternDir(pattern)
			}
			key := m.Pattern + "\x00" + m.Why
			if seen[key] {
				continue
			}
			seen[key] = true
			g.Migrations = append(g.Migrations, m)
			d.in.tag("TO-U1")
			if m.Why == "" {
				d.diag(g.File, codeMigrations, fmt.Sprintf("%s: %s%s", g.File, migrationsMarker, m.Dir))
			} else {
				d.diag(g.File, codeMigrations, fmt.Sprintf("%s: %s(a migrations element at %s:%d is not read: %s)", g.File, migrationsMarker, m.File, m.Line, m.Why))
			}
		}
	}
}

// patternDir is the directory part of a pattern before its first segment
// with a wildcard or a brace.
func patternDir(p string) string {
	segs := strings.Split(p, "/")
	var out []string
	for _, s := range segs[:len(segs)-1] {
		if hasMagic(s) || strings.ContainsAny(s, "{}") {
			break
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return "."
	}
	return strings.Join(out, "/")
}
