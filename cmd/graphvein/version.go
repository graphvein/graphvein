package main

import (
	"runtime/debug"
)

// resolveVersion fills in build metadata that the linker flags did not set.
// Release builds (scripts/build.sh) set version, commit and build date with
// -X; a binary installed with `go install
// github.com/graphvein/graphvein/cmd/graphvein@vX.Y.Z` has none of them, but
// the Go toolchain records the module version it built (and, for a build
// inside a Git checkout, the commit and its time). Without this fallback such
// a binary would report "dev", which makes a wrong-fact report from it
// impossible to place.
//
// Only values still at their defaults are replaced: an injected value always
// wins. A module version of "(devel)" or "" means the toolchain did not know
// it either and is ignored.
func resolveVersion(v, c, d string, read func() (*debug.BuildInfo, bool)) (string, string, string) {
	info, ok := read()
	if !ok || info == nil {
		return v, c, d
	}
	if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	var revision, modified, when string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		case "vcs.time":
			when = s.Value
		}
	}
	if c == "unknown" && revision != "" {
		if len(revision) > 12 {
			revision = revision[:12]
		}
		c = revision
		if modified == "true" {
			c += "-dirty"
		}
	}
	if d == "unknown" && when != "" {
		d = when
	}
	return v, c, d
}
