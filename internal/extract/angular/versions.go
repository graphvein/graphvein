package angular

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

type project struct{ config, name, root string }
type versionInfo struct {
	key   string
	major int
}

func projects(sources map[string][]byte) []project {
	var out []project
	for file, data := range sources {
		if path.Base(file) != "angular.json" {
			continue
		}
		var config struct {
			Projects map[string]struct{ Root, SourceRoot string }
		}
		if typescript.DecodeMetadata(data, &config) != nil {
			continue
		}
		for name, p := range config.Projects {
			root := p.SourceRoot
			if root == "" {
				root = p.Root
			}
			out = append(out, project{file, name, path.Join(path.Dir(file), root)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].config != out[j].config {
			return out[i].config < out[j].config
		}
		return out[i].name < out[j].name
	})
	return out
}

var majorRange = regexp.MustCompile(`^[~^]?((?:0|[1-9][0-9]*))(\.(?:0|[1-9][0-9]*|[xX*]))?(\.(?:0|[1-9][0-9]*|[xX*]))?(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

func majorVersion(v string) int {
	m := majorRange.FindStringSubmatch(v)
	if m == nil {
		return -1
	}
	if strings.ContainsAny(m[2], "xX*") && m[3] != "" && !strings.ContainsAny(m[3], "xX*") {
		return -1
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func (l *linker) version(file string) versionInfo {
	start, key, depth := path.Dir(file), "", -1
	for _, p := range l.projects {
		if (p.root == "." || strings.HasPrefix(file, p.root+"/")) && len(p.root) > depth {
			start, key, depth = path.Dir(p.config), p.config+"#"+p.name, len(p.root)
		}
	}
	if key == "" {
		key = "unowned:" + start
	}
	if v, ok := l.versions[key]; ok {
		return v
	}
	v := versionInfo{key: key, major: -1}
	if depth < 0 {
		v.key = "unowned:."
	}
	for dir := start; ; dir = path.Dir(dir) {
		var pkg struct{ Dependencies, DevDependencies, PeerDependencies map[string]string }
		if data, ok := l.sources[path.Join(dir, "package.json")]; ok && typescript.DecodeMetadata(data, &pkg) == nil {
			deps := map[string]string{}
			for _, group := range []map[string]string{pkg.PeerDependencies, pkg.DevDependencies, pkg.Dependencies} {
				for k, s := range group {
					deps[k] = s
				}
			}
			if _, found := deps["@angular/core"]; found {
				core, compiler := l.lockMajors(dir)
				if !core.found {
					core.major = majorVersion(deps["@angular/core"])
				}
				if !compiler.found {
					compiler.major = majorVersion(deps["@angular/compiler-cli"])
				}
				if core.major >= 0 && core.major == compiler.major {
					v.major = core.major
				}
				if depth < 0 {
					v.key = "package:" + path.Join(dir, "package.json")
				}
				break
			}
		}
		if dir == "." {
			break
		}
	}
	l.versions[key] = v
	return v
}

type lockedMajor struct {
	found bool
	major int
}

func (l *linker) lockMajors(start string) (lockedMajor, lockedMajor) {
	for dir := start; ; dir = path.Dir(dir) {
		var versions [2][]string
		anyLock := false
		for _, name := range []string{"package-lock.json", "bun.lock"} {
			data, ok := l.sources[path.Join(dir, name)]
			if !ok {
				continue
			}
			anyLock = true
			var lock struct {
				LockfileVersion int
				Packages        map[string]json.RawMessage
				Dependencies    map[string]struct{ Version string }
			}
			if typescript.DecodeMetadata(data, &lock) != nil {
				versions[0] = append(versions[0], "invalid")
				versions[1] = append(versions[1], "invalid")
				continue
			}
			for i, dependency := range []string{"@angular/core", "@angular/compiler-cli"} {
				version := ""
				if name == "bun.lock" {
					var items []json.RawMessage
					if json.Unmarshal(lock.Packages[dependency], &items) == nil && len(items) > 0 {
						var text string
						if json.Unmarshal(items[0], &text) == nil {
							version = strings.TrimPrefix(text, dependency+"@")
						}
					}
				} else if lock.LockfileVersion == 1 {
					version = lock.Dependencies[dependency].Version
				} else if lock.LockfileVersion == 2 || lock.LockfileVersion == 3 {
					var entry struct{ Version string }
					if json.Unmarshal(lock.Packages["node_modules/"+dependency], &entry) == nil {
						version = entry.Version
					}
				}
				if version != "" {
					versions[i] = append(versions[i], version)
				}
			}
		}
		if anyLock {
			var result [2]lockedMajor
			for i, values := range versions {
				if len(values) == 0 {
					continue
				}
				result[i] = lockedMajor{true, majorVersion(values[0])}
				for _, m := range values {
					if m != values[0] {
						result[i].major = -1
					}
				}
			}
			return result[0], result[1]
		}
		if dir == "." {
			return lockedMajor{}, lockedMajor{}
		}
	}
}
