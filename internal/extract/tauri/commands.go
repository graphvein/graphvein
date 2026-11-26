package tauri

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// tauriPath reports whether a resolved path is the named item of the tauri
// crate or its macro crate. Clap, poise and unbound attributes resolve
// elsewhere and never qualify.
func tauriPath(external, name, want string) bool {
	return (external == "tauri" || external == "tauri_macros") && name == want
}

// collectCommands applies K1-K3 to every cached command record.
func (l *linker) collectCommands() {
	for _, file := range facts.Paths(l.in.Rust) {
		syntax := l.in.Rust[file].Tauri
		if syntax == nil {
			continue
		}
		for _, c := range syntax.Commands {
			var proven []facts.TauriAttribute
			for _, a := range c.Attributes {
				b := l.in.Resolver.Resolve(file, a.Scope, a.Path)
				if !b.Ambiguous && tauriPath(b.External, b.Name, "command") {
					proven = append(proven, a)
				}
			}
			if len(proven) == 0 {
				continue
			}
			if len(proven) > 1 {
				// Two Tauri attributes do not compile; the function is not a
				// command and its name is unknown.
				l.diagnostic(file, "warning", "tauri_command_invalid", proven[1].Span.StartLine, "command "+c.Name+" has more than one Tauri command attribute")
				continue
			}
			a := proven[0]
			if a.Invalid != "" {
				l.diagnostic(file, "warning", "tauri_command_invalid", a.Span.StartLine, "command "+c.Name+": "+a.Invalid)
			}
			if c.Identity == "" {
				continue
			}
			test := c.Test || a.Form == "cfg_attr" && facts.CfgTestOnly(a.Predicate)
			l.commands[c.Identity] = command{name: invokeName(c, a), conditional: a.Form == "cfg_attr" || c.Cfg || l.functionConditional(file, c.Identity), test: test}
		}
	}
}

// invokeName is K2: the last string rename, else the function name. Any
// invalid argument, a non-string rename and a raw identifier give an unknown
// name, never the identifier.
func invokeName(c facts.TauriCommand, a facts.TauriAttribute) *string {
	if a.Invalid != "" {
		return nil
	}
	name, known := c.Name, !strings.HasPrefix(c.Name, "r#")
	for _, arg := range a.Arguments {
		if arg.Key != "rename" {
			continue
		}
		known = arg.Kind == "string" && arg.Value != nil
		if known {
			name = *arg.Value
		}
	}
	if !known || name == "" || hasControl(name) {
		return nil
	}
	return &name
}

// functionConditional reports the RustFacts Conditional flag of a function
// (an enclosing inline #[cfg] module or a cfg on the item). A function the
// facts do not list is treated as conditional.
func (l *linker) functionConditional(file, identity string) bool {
	if rf := l.in.Rust[file].Rust; rf != nil {
		for _, f := range rf.Functions {
			if f.Identity == identity {
				return f.Conditional
			}
		}
	}
	return true
}
