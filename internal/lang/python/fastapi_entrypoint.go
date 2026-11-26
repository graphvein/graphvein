package python

import (
	"path"
	"regexp"
	"strings"
)

// FastAPIEntrypoint is an app a committed pyproject.toml names as the
// FastAPI CLI's entrypoint (`[tool.fastapi] entrypoint = "pkg.main:app"`):
// `fastapi run` and `fastapi dev` serve that app at its own root. File is
// the scanned module the entrypoint names, Name the module-level binding.
type FastAPIEntrypoint struct {
	Manifest, File, Name string
}

// entrypointText is a FastAPI CLI entrypoint as fastapi-cli 0.0.34 reads
// it (module.submodule:app_name); only a dotted module of identifiers and
// one identifier as the app name are read.
var entrypointText = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*):([A-Za-z_][A-Za-z0-9_]*)$`)

// FastAPIEntrypoints lists the entrypoints of every scanned pyproject.toml
// whose module is a scanned file. fastapi-cli 0.0.34 reads the
// pyproject.toml of the directory it runs in and imports the module with
// that directory first on sys.path, so the module is resolved from the
// manifest's directory only: `<dir>/pkg/main.py` or
// `<dir>/pkg/main/__init__.py`. Any other value is not read.
func (r *ModuleResolver) FastAPIEntrypoints() []FastAPIEntrypoint {
	var out []FastAPIEntrypoint
	for _, dir := range sortedKeys(r.pyprojects) {
		doc, err := parseTOML(r.pyprojects[dir])
		if err != nil {
			continue
		}
		tool, _ := doc["tool"].(map[string]any)
		config, _ := tool["fastapi"].(map[string]any)
		value, _ := config["entrypoint"].(string)
		m := entrypointText.FindStringSubmatch(value)
		if m == nil {
			continue
		}
		// Both a module and a package of the name: not read (the
		// import system's choice is not modelled here).
		module := strings.ReplaceAll(m[1], ".", "/")
		var found []string
		for _, candidate := range []string{module + ".py", module + "/__init__.py"} {
			if file := path.Join(dir, candidate); r.files[file].Python != nil {
				found = append(found, file)
			}
		}
		if len(found) == 1 {
			out = append(out, FastAPIEntrypoint{Manifest: path.Join(dir, "pyproject.toml"), File: found[0], Name: m[2]})
		}
	}
	return out
}
