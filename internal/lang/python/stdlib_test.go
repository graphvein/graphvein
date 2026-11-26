package python

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPythonStdlibNames checks the committed copy of sys.stdlib_module_names
// (stdlib.go, row PS-I5) against the running interpreter
// (GRAPHVEIN_PYTHON_ORACLE, else python3 on PATH) when its version is the
// one the list was read from; it skips otherwise.
func TestPythonStdlibNames(t *testing.T) {
	if !stdlibSet["os"] || stdlibSet["requests"] || externalEcosystem("json") != ecosystemStdlib || externalEcosystem("fastapi") != ecosystemPyPI {
		t.Error("ecosystem choice does not follow the list")
	}
	interpreter := os.Getenv("GRAPHVEIN_PYTHON_ORACLE")
	if interpreter == "" {
		found, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("no Python interpreter on PATH")
		}
		interpreter = found
	}
	out, err := exec.Command(interpreter, "-I", "-c", "import sys; print('%d.%d' % sys.version_info[:2]); print(' '.join(sorted(sys.stdlib_module_names)))").Output()
	if err != nil {
		t.Skipf("%s does not run: %v", interpreter, err)
	}
	version, names, _ := strings.Cut(string(out), "\n")
	if strings.TrimSpace(version) != stdlibNamesVersion {
		t.Skipf("%s is Python %s; the list was read from %s", interpreter, strings.TrimSpace(version), stdlibNamesVersion)
	}
	if got, want := strings.Join(strings.Fields(stdlibNames), " "), strings.Join(strings.Fields(names), " "); got != want {
		t.Errorf("stdlib.go differs from %s's sys.stdlib_module_names; regenerate it (command in stdlib.go)", interpreter)
	}
}

// TestPythonMetaPathNames checks metapath.go (rows PS-I4, PS-I5, PS-F2's
// encoding_cookie) against the running interpreter, read by
// scripts/oracle/python_structure/stdlib_lists.py: exactly when it is the
// CPython the lists were generated from; otherwise, for any CPython 3.14,
// that its frozen and start-up modules are meta-path names (the same on
// every build; a start-up built-in may be a platform name), its built-in
// modules are meta-path or platform names, and
// no pure-Python module of its standard library is a platform name (but
// the parents of frozen submodules).
func TestPythonMetaPathNames(t *testing.T) {
	if !metaPathSet["sys"] || !metaPathSet["os"] || !platformSet["math"] || metaPathSet["json"] || platformSet["json"] || !interpreterSet["__main__"] || externalEcosystem("test") != ecosystemStdlib {
		t.Error("the committed lists do not hold the expected names")
	}
	interpreter := os.Getenv("GRAPHVEIN_PYTHON_ORACLE")
	if interpreter == "" {
		found, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("no Python interpreter on PATH")
		}
		interpreter = found
	}
	out, err := exec.Command(interpreter, "-I", "../../../scripts/oracle/python_structure/stdlib_lists.py").Output()
	if err != nil {
		t.Skipf("%s does not run the list script: %v", interpreter, err)
	}
	var got struct {
		Version     string   `json:"version"`
		Builtin     []string `json:"builtin"`
		Frozen      []string `json:"frozen"`
		Startup     []string `json:"startup"`
		MetaPath    []string `json:"meta_path"`
		Platform    []string `json:"platform"`
		Pure        []string `json:"pure"`
		Parents     []string `json:"frozen_parents"`
		Shipped     []string `json:"shipped"`
		ASCIICodecs []string `json:"ascii_codecs"`
		UTF8Codecs  []string `json:"utf8_codecs"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Version, stdlibNamesVersion+".") {
		t.Skipf("%s is Python %s; the lists were read from %s", interpreter, got.Version, metaPathNamesVersion)
	}
	join := func(names []string) string { return strings.Join(names, " ") }
	if got.Version == metaPathNamesVersion {
		for _, c := range []struct {
			name, committed string
			read            []string
		}{
			{"metaPathNames", metaPathNames, got.MetaPath}, {"platformModuleNames", platformModuleNames, got.Platform},
			{"shippedModuleNames", shippedModuleNames, got.Shipped}, {"asciiCodecNames", asciiCodecNames, got.ASCIICodecs},
			{"utf8CodecNames", utf8CodecNames, got.UTF8Codecs},
		} {
			if join(strings.Fields(c.committed)) != join(c.read) {
				t.Errorf("metapath.go %s differs from %s; regenerate it (stdlib_lists.py --go)", c.name, interpreter)
			}
		}
		return
	}
	builtin := nameSet(strings.Join(got.Builtin, " "))
	for _, name := range append(append([]string{}, got.Frozen...), got.Startup...) {
		// A built-in loaded at start-up on another platform (nt) may be a
		// platform name: refused there, never a repository file.
		if !metaPathSet[name] && !(builtin[name] && platformSet[name]) {
			t.Errorf("%s: %s is frozen or loaded at start-up, not a meta-path name", interpreter, name)
		}
	}
	for _, name := range got.Builtin {
		if !metaPathSet[name] && !platformSet[name] {
			t.Errorf("%s: built-in %s is neither a meta-path nor a platform name", interpreter, name)
		}
	}
	pure := nameSet(strings.Join(got.Pure, " "))
	parents := nameSet(strings.Join(got.Parents, " "))
	for name := range platformSet {
		if pure[name] && !parents[name] {
			t.Errorf("%s: platform name %s is a pure-Python module there", interpreter, name)
		}
	}
}
