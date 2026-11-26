// Package rust extracts Rust declarations and conservative crate-local links.
package rust

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func compact(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// compactType removes the white space of a type's text (docs/lang/identity.md,
// "Rust"), keeping one space where two words would otherwise run together
// (`dyn m::Tr`, `*const T`, `&'a u8`), so that no two types share a name.
func compactType(s string) string {
	word := func(r rune) bool { return r == '_' || r >= 0x80 || unicode.IsLetter(r) || unicode.IsDigit(r) }
	var b strings.Builder
	space := false
	last := rune(0)
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && last != 0 && word(last) && word(r) {
			b.WriteByte(' ')
		}
		space = false
		last = r
		b.WriteRune(r)
	}
	return b.String()
}

func selfType(n *parse.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case "reference_type":
		return selfType(n.Fields["type"])
	case "type_identifier":
		return n.Text
	case "scoped_type_identifier":
		return facts.Text(n.Fields["name"])
	case "generic_type":
		base := selfType(n.Fields["type"])
		// Indirection/container self types retain their full text, not the
		// contained type's identity. User-defined generic types drop arguments.
		if !facts.Builtin(base, "Box Rc Arc Pin Cow") {
			return base
		}
	}
	return compactType(n.Text)
}

func receiverPath(n *parse.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == "reference_type" {
		return receiverPath(n.Fields["type"])
	}
	if n.Kind == "generic_type" && !facts.Builtin(selfType(n.Fields["type"]), "Box Rc Arc Pin Cow") {
		return receiverPath(n.Fields["type"])
	}
	return compactType(n.Text)
}

func usePaths(n *parse.Node, prefix string, scope string, span contract.Span) []facts.Import {
	if n == nil {
		return nil
	}
	join := func(s string) string {
		if prefix == "" {
			return s
		}
		return prefix + "::" + s
	}
	switch n.Kind {
	case "scoped_use_list":
		return usePaths(n.Fields["list"], join(facts.Text(n.Fields["path"])), scope, span)
	case "use_list":
		var out []facts.Import
		for _, c := range n.Children {
			out = append(out, usePaths(c, prefix, scope, span)...)
		}
		return out
	case "use_as_clause":
		p := join(facts.Text(n.Fields["path"]))
		return []facts.Import{{Module: p, Bindings: map[string]string{facts.Text(n.Fields["alias"]): p}, Scope: scope, Span: span}}
	default:
		p := join(n.Text)
		parts := strings.Split(p, "::")
		alias := parts[len(parts)-1]
		if alias == "self" && len(parts) > 1 {
			alias = parts[len(parts)-2]
			p = strings.TrimSuffix(p, "::self")
		}
		return []facts.Import{{Module: p, Bindings: map[string]string{alias: p}, Scope: scope, Span: span}}
	}
}

// walkCtx is the position of a node during extraction.
type walkCtx struct {
	module, owner, caller, typ, trait string
	// local marks a position inside a function body, closure or other
	// block: items there are not module items; they are claimed under fn
	// (row RS-D10) or claim nothing (RS-D12).
	local bool
	// inImpl marks the direct items of an impl block (rows RS-D5, RS-D9).
	inImpl bool
	// inForeign marks the direct items of an extern block (RS-D7, RS-D8).
	inForeign bool
	// blockModule marks a position inside a mod item in a block (RS-M5).
	blockModule bool
	// localFn is the span of the innermost local function (RS-D10) around
	// the position, nil outside one.
	localFn *contract.Span
	// fn is the claimed function (or, for its direct items, the claimed
	// trait inside a function body) whose body holds the position: items
	// there are claimed under it (row RS-D10). It is nil at module level
	// and wherever items claim nothing (const and static initializers
	// outside function bodies, block mods, the bodies of refused items).
	fn *localOwner
	// inTrait marks the direct items of a trait inside a function body.
	inTrait bool
}

// localOwner is the lexical owner of the items in a function body (row
// RS-D10): a claimed function, method or trait.
type localOwner struct {
	// base is the owner's qualified name without its @n suffix: the
	// prefix of the qualified names of the items it holds.
	base string
	// id is the owner's identity when it is a module item; pending (index
	// + 1 into pendingItems) when it is itself an item of the second
	// numbering pass, whose identity is known only after the walk.
	id      string
	pending int
	// trait is the name of a claimed trait inside a function body, for
	// the receiver attributes of its methods (as for module-level traits).
	trait string
}

// pendingItem is a claimed declaration of the second numbering pass: an
// impl associated const or type (row RS-D9) or an item in a function body
// (row RS-D10). These take their @n after every module item of the file
// (rows RS-D1..RS-D8), so they never change a module item's identity.
type pendingItem struct {
	n                *parse.Node
	kind, name, base string
	owner            localOwner
	attrs            map[string]any
	id               string
}

// Extract keeps cfg alternatives and syntax-backed candidates without
// evaluating Rust. Declarations follow the Rust structural claim contract
// (docs/lang/rust-structure.md): module items, impl associated consts and
// types, and items in function bodies named under their function; items in
// other blocks (const and static initializers, block mods) go to the default
// branch, counted in facts.RustFacts.Unread.
func Extract(file string, source []byte, root *parse.Node) facts.Facts {
	structureTag("RS-F1")
	out := facts.Facts{Exports: map[string]string{}}
	counts := map[string]int{}
	unread := map[string]int{}
	var aliases []string
	var pending []pendingItem
	currentReceiver := ""
	queries := &queryCapture{}
	var visit func(*parse.Node, walkCtx)
	visit = func(n *parse.Node, w walkCtx) {
		if n == nil {
			return
		}
		module, owner, caller, typ, trait := w.module, w.owner, w.caller, w.typ, w.trait
		switch n.Kind {
		case "mod_item":
			name := facts.Text(n.Fields["name"])
			body := n.Fields["body"]
			if w.local {
				// Row RS-M5: a block's mod item is no module of the file.
				structureTag("RS-M5")
				structureSite("D", siteMod, file, facts.Span(n))
				unread[unreadBlockModule]++
				inner := w
				inner.blockModule = true
				inner.inImpl, inner.inForeign = false, false
				visit(body, inner)
				return
			}
			out.Modules = append(out.Modules, facts.Module{Name: name, Scope: module, Inline: body != nil, Span: facts.Span(n)})
			if body != nil {
				inner := w
				inner.module, inner.typ, inner.trait = facts.Join(module, name), "", ""
				inner.inImpl, inner.inForeign = false, false
				visit(body, inner)
			}
			return
		case "use_declaration":
			if w.blockModule {
				structureSite("D", siteUse, file, facts.Span(n))
				unread[unreadBlockModule]++
				return
			}
			imports := usePaths(n.Fields["argument"], "", lexicalScope(caller, module), facts.Span(n))
			for i := range imports {
				imports[i].ModuleScope = module
			}
			out.Imports = append(out.Imports, imports...)
			return
		case "extern_crate_declaration":
			if alias := n.Fields["alias"]; alias != nil && !w.local {
				aliases = append(aliases, alias.Text)
			}
			return
		case "impl_item":
			previous := currentReceiver
			currentReceiver = receiverPath(n.Fields["type"])
			inner := w
			inner.typ, inner.trait = selfType(n.Fields["type"]), selfType(n.Fields["trait"])
			inner.inImpl, inner.inForeign = true, false
			visit(n.Fields["body"], inner)
			currentReceiver = previous
			return
		case "foreign_mod_item":
			inner := w
			inner.inForeign, inner.inImpl = true, false
			visit(n.Fields["body"], inner)
			return
		case "macro_invocation":
			if !w.local && !w.blockModule {
				structureTag("RS-D11")
			}
			queries.macro(n, caller, w.localFn)
			return
		}
		kind := map[string]string{"function_item": "function", "function_signature_item": "function", "struct_item": "struct", "enum_item": "enum", "union_item": "union", "trait_item": "trait", "type_item": "type_alias", "const_item": "constant", "static_item": "variable", "macro_definition": "macro"}[n.Kind]
		if kind != "" {
			name := facts.Text(n.Fields["name"])
			if name == "" {
				return
			}
			if w.blockModule || w.local && w.fn == nil {
				// Row RS-D12: an item in a block outside any claimed
				// function body (a const or static initializer, a block
				// mod, the body of an item that claims nothing) is no
				// item of the file. Its calls stay with the enclosing
				// function.
				structureTag("RS-D12")
				structureSite("D", siteDeclaration, file, facts.Span(n))
				unread[unreadLocalItem]++
				inner := w
				inner.inImpl, inner.inForeign, inner.inTrait, inner.fn = false, false, false, nil
				switch n.Kind {
				case "function_item":
					inner.local = true
					span := facts.Span(n)
					inner.localFn = &span
					visit(n.Fields["body"], inner)
				case "trait_item":
					visit(n.Fields["body"], inner)
				case "const_item", "static_item":
					countLocal(file, n.Fields["value"], unread)
				}
				return
			}
			// local: an item in a claimed function body (row RS-D10), named
			// under that function and contained by it.
			local := w.local
			assoc := w.inImpl && (kind == "constant" || kind == "type_alias" || kind == "variable")
			if assoc {
				// Row RS-D9: an impl's associated const or type is a member
				// of the impl's self type, as its methods are. A `static`
				// (not Rust) and `const _` (rustc: "`const` items in this
				// context need a name") claim nothing.
				countLocal(file, n.Fields["value"], unread)
				if kind == "variable" || name == "_" {
					structureSite("RS-D9", siteDeclaration, file, facts.Span(n))
					unread[unreadImplAssoc]++
					return
				}
				structureTag("RS-D9")
			} else if kind == "constant" || kind == "variable" {
				public := false
				for _, c := range n.Children {
					public = public || c.Kind == "visibility_modifier"
				}
				countLocal(file, n.Fields["value"], unread)
				if !public || name == "_" {
					return
				}
				if !local && w.inForeign {
					structureTag("RS-D8")
				} else if !local {
					structureTag("RS-D4")
				}
			}
			prefix := module
			if local {
				prefix = w.fn.base
			}
			q := facts.Join(prefix, name)
			// member: a direct item of an impl block, or of a trait (at
			// module level typ is set exactly there; in a function body
			// typ keeps the enclosing method's self type, so the flags
			// decide).
			member := typ != "" && !local || local && (w.inImpl || w.inTrait)
			receiver, receiverTrait, origin := typ, trait, currentReceiver
			if local && w.inTrait {
				receiver, receiverTrait, origin = w.fn.trait, "", ""
			}
			if member && (kind == "function" || assoc) {
				if kind == "function" {
					kind = "method"
				}
				if !local || w.inImpl {
					q = facts.Join(prefix, typ+"."+name)
					if trait != "" {
						q += "@" + trait
					}
				}
			}
			switch {
			case local:
				structureTag("RS-D10")
			case assoc:
			case kind == "method" && w.inImpl:
				structureTag("RS-D5")
			case kind == "method":
				structureTag("RS-D6")
			case kind == "function" && w.inForeign:
				structureTag("RS-D7")
			case kind == "function":
				structureTag("RS-D1")
			case kind == "macro":
				structureTag("RS-D3")
			case kind != "constant" && kind != "variable":
				structureTag("RS-D2")
			}
			attrs := map[string]any{"module": module}
			if member {
				attrs["receiver"] = receiver
				attrs["trait"] = receiverTrait
				if origin == "" {
					origin = receiver
				}
				attrs["receiver_path"] = origin
			}
			if local || assoc {
				// Second numbering pass: the node is added after the walk.
				ref := localOwner{id: owner}
				if local {
					attrs["local"] = true
					ref = *w.fn
					ref.trait = ""
				}
				pending = append(pending, pendingItem{n: n, kind: kind, name: name, base: q, owner: ref, attrs: attrs})
				self := &localOwner{base: q, pending: len(pending)}
				inner := w
				inner.inImpl, inner.inForeign, inner.inTrait = false, false, false
				switch n.Kind {
				case "function_item":
					inner.local = true
					span := facts.Span(n)
					inner.localFn = &span
					inner.fn = self
					visit(n.Fields["body"], inner)
				case "trait_item":
					self.trait = name
					inner.fn = self
					inner.inTrait = true
					visit(n.Fields["body"], inner)
				}
				return
			}
			base := q
			counts[q]++
			if counts[q] > 1 {
				q += "@" + strconv.Itoa(counts[q])
			}
			id := facts.Add(&out, file, source, n, kind, name, q, owner, attrs)
			out.Exports[q] = id
			if kind == "trait" {
				visit(n.Fields["body"], walkCtx{module: module, owner: id, caller: id, typ: name})
				return
			}
			if kind == "function" || kind == "method" {
				firstCall := len(out.Calls)
				visit(n.Fields["body"], walkCtx{module: module, owner: id, caller: id, typ: typ, trait: trait, local: true, fn: &localOwner{base: base, id: id}})
				shadow := map[string]bool{}
				var names func(*parse.Node)
				names = func(p *parse.Node) {
					if p == nil {
						return
					}
					if p.Kind == "identifier" {
						shadow[p.Text] = true
						return
					}
					for _, c := range p.Children {
						names(c)
					}
				}
				if params := n.Fields["parameters"]; params != nil {
					for _, p := range params.Children {
						names(p.Fields["pattern"])
					}
				}
				var locals func(*parse.Node)
				locals = func(p *parse.Node) {
					if p == nil || p.Kind == "function_item" || p.Kind == "mod_item" {
						return
					}
					if p.Kind == "let_declaration" || p.Kind == "for_expression" {
						names(p.Fields["pattern"])
					}
					if p.Kind == "closure_expression" {
						names(p.Fields["parameters"])
					}
					for _, c := range p.Children {
						locals(c)
					}
				}
				locals(n.Fields["body"])
				for i := firstCall; i < len(out.Calls); i++ {
					base := strings.Split(out.Calls[i].Name, "::")[0]
					out.Calls[i].Shadowed = shadow[base]
				}
			}
			return
		}
		if n.Kind == "call_expression" {
			queries.call(n, caller, w.localFn)
			callee := n.Fields["function"]
			name := ""
			staticName := ""
			if callee != nil {
				switch callee.Kind {
				case "identifier", "scoped_identifier":
					name = callee.Text
				case "generic_function":
					name = facts.Text(callee.Fields["function"])
					if inner := callee.Fields["function"]; inner != nil && inner.Kind == "field_expression" {
						staticName = facts.Text(inner.Fields["field"])
					}
				case "field_expression":
					staticName = facts.Text(callee.Fields["field"])
					if facts.Text(callee.Fields["value"]) == "self" && typ != "" {
						name = typ + "::" + facts.Text(callee.Fields["field"])
					}
				}
			}
			name = strings.ReplaceAll(name, "Self::", typ+"::")
			out.Calls = append(out.Calls, facts.Call{StaticName: staticName, Source: caller, Scope: lexicalScope(caller, module), ModuleScope: module, Name: name, Span: facts.Span(n)})
		}
		// Attributes are siblings of declarations. Retain their decoded path
		// for resolution against the current inventory, including literal spaces.
		pendingPath := ""
		inner := w
		if n.Kind != "declaration_list" && n.Kind != "source_file" {
			// Only an impl, extern block or module body's own items are
			// direct items; anything nested deeper is not.
			inner.inImpl, inner.inForeign, inner.inTrait = false, false, false
		}
		for _, c := range n.Children {
			if c.Kind == "attribute_item" {
				if strings.HasPrefix(compact(c.Text), "#[path=") {
					text := strings.TrimSpace(strings.TrimSuffix(c.Text[strings.Index(c.Text, "=")+1:], "]"))
					pendingPath, _ = StringLiteral(text)
				}
				continue
			}
			if c.Kind == "line_comment" || c.Kind == "block_comment" {
				// Comments, doc comments included, between an item's
				// outer attributes and the item do not detach them
				// (rustc: they are attributes or trivia of the item).
				continue
			}
			firstModule := len(out.Modules)
			visit(c, inner)
			if c.Kind == "mod_item" && pendingPath != "" && firstModule < len(out.Modules) {
				out.Modules[firstModule].Path = pendingPath
			}
			pendingPath = ""
		}
	}
	visit(root, walkCtx{caller: facts.ID("file", file, file)})
	out.Rust = syntax(file, source, root, out)
	if len(unread) > 0 {
		out.Rust.Unread = unread
	}
	out.Rust.ExternAliases = aliases
	out.Rust.Shape = safeCaptureShape(source, root, out, out.Rust.Scopes)
	queries.finish(out.Imports, out.Rust)
	out.Tauri = captureTauri(file, source, root, out)
	// Second numbering pass (rows RS-D9, RS-D10), after the syntax scopes
	// and the Tauri capture, which read module items only: an item in a
	// function body binds no scope name and gives a Tauri command no
	// identity, as before it was claimed; calls keep the enclosing module
	// item as their caller.
	for i := range pending {
		p := &pending[i]
		q := p.base
		counts[q]++
		if counts[q] > 1 {
			q += "@" + strconv.Itoa(counts[q])
		}
		owner := p.owner.id
		if p.owner.pending > 0 {
			owner = pending[p.owner.pending-1].id
		}
		p.id = facts.Add(&out, file, source, p.n, p.kind, p.name, q, owner, p.attrs)
	}
	return out
}

// countLocal counts the items a const or static initializer holds (row
// RS-D12: items in blocks are no module items) without capturing anything.
func countLocal(file string, n *parse.Node, unread map[string]int) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "mod_item":
		structureTag("RS-M5")
		structureSite("D", siteMod, file, facts.Span(n))
		unread[unreadBlockModule]++
	case "function_item", "function_signature_item", "struct_item", "enum_item", "union_item", "trait_item", "type_item", "const_item", "static_item", "macro_definition":
		if n.Fields["name"] != nil {
			structureTag("RS-D12")
			structureSite("D", siteDeclaration, file, facts.Span(n))
			unread[unreadLocalItem]++
		}
	}
	for _, c := range n.Children {
		countLocal(file, c, unread)
	}
}

// module is one module instance of a crate's tree: the file holding it,
// its inline module scope in that file ("" for the file itself), the
// directory its outlined `mod` items load from, and for an inline module the
// span of its `mod` item (two inline modules with one name in one file are
// two instances).
type module struct {
	file, scope, dir string
	inline           bool
	span             contract.Span
}

func lexicalScope(caller, module string) string {
	kind, _, _, _ := contract.ParseIdentity(caller)
	if kind == "function" || kind == "method" {
		return caller
	}
	return module
}

// crate is a crate's module tree by qualified name. alts lists, per
// qualified name with several `mod` items (cfg alternatives), the names of
// every alternative: the plain name, then `<name>#2`, `#3`, ...
type crate struct {
	modules map[string]module
	alts    map[string][]string
}

// moduleNames returns, sorted, the qualified names under which a crate's
// tree holds the module scope of a file (several when the file is reached
// as several modules). at selects the inline module instance holding a
// position; the zero span accepts any.
func moduleNames(c *crate, file, scope string, at contract.Span) []string {
	var out []string
	for q, m := range c.modules {
		if m.file == file && m.scope == scope && (!m.inline || at == (contract.Span{}) || within(at, m.span)) {
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return out
}

func attribute(n contract.Node, k string) string { s, _ := n.Attributes[k].(string); return s }

// localNode reports whether a node is an item of a function body (row
// RS-D10, attribute `local`). Such items are no module items: the file's
// `macro_rules!` names that shadow a crate (RS-I3) and the methods that
// method calls resolve to are module items only, as before these items
// were claimed.
func localNode(n contract.Node) bool { b, _ := n.Attributes["local"].(bool); return b }

// Link rebuilds Rust edges using current scanned Cargo metadata.
func Link(files map[string]facts.Facts, sources map[string][]byte) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkWithResolver(files, NewModuleResolver(files, sources))
}
