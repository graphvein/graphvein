package typescript

import (
	"encoding/json"
	"path"
	"strings"
)

// This file reads the compiler options a model linker needs besides module
// resolution (T018g6, row TO-V3): TypeORM's column types come from
// the decorator metadata tsc emits, which exists only with
// experimentalDecorators and emitDecoratorMetadata, and whose value for a
// `T | null` annotation depends on strictNullChecks. The
// options are read from the same tsconfig chain the resolver uses for paths.

// compilerFlagNames are the boolean compilerOptions CompilerFlags reports.
var compilerFlagNames = []string{"experimentalDecorators", "emitDecoratorMetadata", "strict", "strictNullChecks"}

// CompilerFlags are the boolean compiler options of compilerFlagNames as
// the tsconfig chain nearest to a file states them.
type CompilerFlags struct {
	// Config is the nearest tsconfig.json or jsconfig.json; "" when there
	// is none.
	Config string
	// Values holds each option the chain sets: true or false, or nil when
	// the nearest config that sets it does not use a boolean literal. An
	// option no config of the chain sets is absent.
	Values map[string]*bool
	// Unresolved lists the configs of the chain that are not read: an
	// `extends` entry that is not a relative path to a config of the
	// repository (a package such as "@tsconfig/node20"), or a config that
	// does not parse. An option absent from Values may be set there.
	Unresolved []string
}

// configFlags reads compilerFlagNames from a config's JSON (comments
// already removed); values other than true or false are kept as nil.
func configFlags(data []byte) map[string]*bool {
	var raw struct {
		Options map[string]json.RawMessage `json:"compilerOptions"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	out := map[string]*bool{}
	for _, name := range compilerFlagNames {
		v, ok := raw.Options[name]
		if !ok {
			continue
		}
		var b bool
		if json.Unmarshal(v, &b) == nil {
			out[name] = &b
		} else {
			out[name] = nil
		}
	}
	return out
}

// CompilerFlags returns the options of compilerFlagNames for file: the
// nearest tsconfig.json (else jsconfig.json) above it, then its relative
// `extends` chain, a child's setting overriding its parents' (later
// entries of an `extends` array override earlier ones, as tsc merges
// them).
func (r *ModuleResolver) CompilerFlags(file string) CompilerFlags {
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		for dir := path.Dir(file); ; dir = path.Dir(dir) {
			p := path.Join(dir, name)
			if _, ok := r.configs[p]; ok {
				out := CompilerFlags{Config: p, Values: map[string]*bool{}}
				r.flagsAt(p, &out, map[string]bool{})
				return out
			}
			if r.badConfigs[p] {
				return CompilerFlags{Config: p, Values: map[string]*bool{}, Unresolved: []string{p}}
			}
			if dir == "." || dir == "/" {
				break
			}
		}
	}
	return CompilerFlags{Values: map[string]*bool{}}
}

// flagsAt merges the flags of file's chain into out: parents first, then
// file's own settings.
func (r *ModuleResolver) flagsAt(file string, out *CompilerFlags, seen map[string]bool) {
	if seen[file] {
		return
	}
	seen[file] = true
	defer delete(seen, file)
	c := r.configs[file]
	for _, parent := range c.parents {
		if !strings.HasPrefix(parent, ".") {
			// A later parent overrides what earlier ones set, so nothing
			// set so far is proven.
			out.Unresolved = append(out.Unresolved, parent)
			clear(out.Values)
			continue
		}
		base := path.Clean(path.Join(path.Dir(file), parent))
		found := false
		for _, p := range []string{base, base + ".json", path.Join(base, "tsconfig.json")} {
			if _, ok := r.configs[p]; ok {
				r.flagsAt(p, out, seen)
				found = true
				break
			}
		}
		if !found {
			out.Unresolved = append(out.Unresolved, base)
			clear(out.Values)
		}
	}
	for name, v := range c.flags {
		out.Values[name] = v
	}
}
