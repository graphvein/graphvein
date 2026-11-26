package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		{Key: "vcs.time", Value: "2026-11-20T10:00:00Z"},
		{Key: "vcs.modified", Value: "false"},
	}
	dirty := append(append([]debug.BuildSetting{}, vcs[:3]...), debug.BuildSetting{Key: "vcs.modified", Value: "true"})
	tests := []struct {
		name             string
		v, c, d          string
		info             *debug.BuildInfo
		ok               bool
		wantV, wantC, wd string
	}{
		{"go install of a tag", "dev", "unknown", "unknown",
			&debug.BuildInfo{Main: debug.Module{Path: "github.com/graphvein/graphvein", Version: "v0.1.0"}}, true,
			"v0.1.0", "unknown", "unknown"},
		{"build in a checkout", "dev", "unknown", "unknown",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}, Settings: vcs}, true,
			"v0.1.0", "0123456789ab", "2026-11-20T10:00:00Z"},
		{"dirty checkout", "dev", "unknown", "unknown",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.1.1-0.20261120100000-0123456789ab+dirty"}, Settings: dirty}, true,
			"v0.1.1-0.20261120100000-0123456789ab+dirty", "0123456789ab-dirty", "2026-11-20T10:00:00Z"},
		{"go run (devel)", "dev", "unknown", "unknown",
			&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true,
			"dev", "unknown", "unknown"},
		{"injected values win", "v0.2.0", "abc1234", "2026-11-21T00:00:00Z",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}, Settings: vcs}, true,
			"v0.2.0", "abc1234", "2026-11-21T00:00:00Z"},
		{"no build info", "dev", "unknown", "unknown", nil, false,
			"dev", "unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := func() (*debug.BuildInfo, bool) { return tt.info, tt.ok }
			v, c, d := resolveVersion(tt.v, tt.c, tt.d, read)
			if v != tt.wantV || c != tt.wantC || d != tt.wd {
				t.Errorf("got %q %q %q, want %q %q %q", v, c, d, tt.wantV, tt.wantC, tt.wd)
			}
		})
	}
}
