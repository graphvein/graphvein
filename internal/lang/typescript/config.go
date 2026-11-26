package typescript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// jsonc removes only trivia, preserving strings and newlines for JSON diagnostics.
func jsonc(source []byte) ([]byte, error) {
	data := append([]byte(nil), bytes.TrimPrefix(source, []byte{0xef, 0xbb, 0xbf})...)
	quoted := false
	for i := 0; i < len(data); i++ {
		if quoted {
			if data[i] == '\\' {
				i++
			} else if data[i] == '"' {
				quoted = false
			}
			continue
		}
		if data[i] == '"' {
			quoted = true
			continue
		}
		if data[i] != '/' || i+1 >= len(data) {
			continue
		}
		if data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				data[i] = ' '
				i++
			}
		} else if data[i+1] == '*' {
			data[i], data[i+1] = ' ', ' '
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				if data[i] != '\n' && data[i] != '\r' {
					data[i] = ' '
				}
				i++
			}
			if i+1 >= len(data) {
				return nil, fmt.Errorf("unterminated block comment")
			}
			data[i], data[i+1] = ' ', ' '
			i++
		}
	}
	quoted = false
	for i := 0; i < len(data); i++ {
		if quoted {
			if data[i] == '\\' {
				i++
			} else if data[i] == '"' {
				quoted = false
			}
			continue
		}
		if data[i] == '"' {
			quoted = true
			continue
		}
		if data[i] == ',' {
			j := i + 1
			for j < len(data) && strings.ContainsRune(" \t\r\n", rune(data[j])) {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				data[i] = ' '
			}
		}
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSONC")
	}
	return data, nil
}

// objectKeys preserves source order for TypeScript's condition and pattern ties.
func objectKeys(data []byte) []string {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	var keys []string
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil
		}
		keys = append(keys, key)
	}
	return keys
}

type pathPattern struct {
	name    string
	targets []string
}
type moduleConfig struct {
	base, pathsBase   string
	hasBase, hasPaths bool
	patterns          []pathPattern
	parents           []string
	// flags are the compilerOptions of compilerFlagNames this file sets to
	// a boolean literal (CompilerFlags); a key set to anything else maps
	// to nil.
	flags map[string]*bool
}

func parseConfig(file string, source []byte) (moduleConfig, error) {
	var out moduleConfig
	data, err := jsonc(source)
	if err != nil {
		return out, err
	}
	var raw struct {
		Extends json.RawMessage `json:"extends"`
		Options struct {
			Base  *string         `json:"baseUrl"`
			Paths json.RawMessage `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := decodeObject(data, &raw); err != nil {
		return out, err
	}
	out.flags = configFlags(data)
	if raw.Options.Base != nil {
		out.base, out.hasBase = path.Clean(path.Join(path.Dir(file), *raw.Options.Base)), true
	}
	if raw.Options.Paths != nil {
		var patterns map[string][]string
		if err := json.Unmarshal(raw.Options.Paths, &patterns); err != nil {
			return out, err
		}
		out.hasPaths, out.pathsBase = true, path.Dir(file)
		for _, key := range objectKeys(raw.Options.Paths) {
			if strings.Count(key, "*") > 1 {
				continue
			}
			out.patterns = append(out.patterns, pathPattern{key, patterns[key]})
		}
	}
	if len(raw.Extends) > 0 {
		var parent string
		if json.Unmarshal(raw.Extends, &parent) == nil {
			out.parents = []string{parent}
		} else {
			if err := json.Unmarshal(raw.Extends, &out.parents); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func (r *ModuleResolver) configAt(file string, seen map[string]bool) moduleConfig {
	if seen[file] {
		return moduleConfig{}
	}
	seen[file] = true
	defer delete(seen, file)
	child := r.configs[file]
	var result moduleConfig
	for _, parent := range child.parents {
		if !strings.HasPrefix(parent, ".") {
			continue
		}
		base := path.Clean(path.Join(path.Dir(file), parent))
		for _, p := range []string{base, base + ".json", path.Join(base, "tsconfig.json")} {
			if _, ok := r.configs[p]; ok {
				inherited := r.configAt(p, seen)
				if inherited.hasBase {
					result.base, result.hasBase = inherited.base, true
				}
				if inherited.hasPaths {
					result.patterns, result.pathsBase, result.hasPaths = inherited.patterns, inherited.pathsBase, true
				}
				break
			}
		}
	}
	if child.hasBase {
		result.base, result.hasBase = child.base, true
	}
	if child.hasPaths {
		result.patterns, result.pathsBase, result.hasPaths = child.patterns, child.pathsBase, true
	}
	return result
}

func (r *ModuleResolver) config(file string) moduleConfig {
	if c, ok := r.fileConfigs[file]; ok {
		return c
	}
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		for dir := path.Dir(file); ; dir = path.Dir(dir) {
			p := path.Join(dir, name)
			if _, ok := r.configs[p]; ok {
				c := r.configAt(p, map[string]bool{})
				r.fileConfigs[file] = c
				return c
			}
			if dir == "." || dir == "/" {
				break
			}
		}
	}
	r.fileConfigs[file] = moduleConfig{}
	return moduleConfig{}
}

func pathMatch(pattern, module string) (string, bool) {
	before, after, star := strings.Cut(pattern, "*")
	if !star {
		return "", pattern == module
	}
	if len(module) < len(before)+len(after) || !strings.HasPrefix(module, before) || !strings.HasSuffix(module, after) {
		return "", false
	}
	return module[len(before) : len(module)-len(after)], true
}

func bestPattern(patterns []pathPattern, module string) (pathPattern, string, bool) {
	var best pathPattern
	var capture string
	found, prefix, suffix := false, -1, -1
	for _, p := range patterns {
		match, ok := pathMatch(p.name, module)
		if !ok {
			continue
		}
		before, after, star := strings.Cut(p.name, "*")
		if !star {
			return p, match, true
		}
		if !found || len(before) > prefix || len(before) == prefix && len(after) > suffix {
			best, capture, found, prefix, suffix = p, match, true, len(before), len(after)
		}
	}
	return best, capture, found
}

// decodeObject rejects nonobjects and trailing values rather than hiding bad config.
func decodeObject(data []byte, target any) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return fmt.Errorf("expected an object")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}
