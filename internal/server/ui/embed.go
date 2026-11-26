// Package ui holds the built web UI that graphvein serve embeds. The files
// in static/ are committed so `go build` works without Node (ADR 0041).
package ui

import (
	"embed"
	"io/fs"
)

// static uses the all: prefix because without it go:embed silently drops
// files whose names start with "." or "_", and bundler hashes can start
// with "_".
//
//go:embed all:static
var static embed.FS

// legal holds copies of the repository's NOTICE and THIRD_PARTY_NOTICES
// (ADR 0080), so the binary carries the notices of everything it bundles,
// not only the web UI's. go:embed cannot reach the repository root, so the
// copies are committed here; TestLegalCopies keeps them equal to the
// originals, and `go run ./scripts/notices` rewrites both notice files.
//
//go:embed legal
var legal embed.FS

// legalFiles are the root names under which the legal copies are served.
var legalFiles = map[string]bool{"NOTICE.txt": true, "THIRD_PARTY_NOTICES.txt": true}

// Assets is the UI file tree rooted at static/, so index.html is at its
// root and hashed bundles are under assets/. NOTICE.txt and
// THIRD_PARTY_NOTICES.txt are added at its root.
var Assets fs.FS = overlay{base: mustSub(static, "static"), extra: mustSub(legal, "legal")}

// overlay serves the names in legalFiles from extra and everything else
// from base. It does not merge directory listings: the server never lists
// directories.
type overlay struct {
	base, extra fs.FS
}

func (o overlay) Open(name string) (fs.File, error) {
	if legalFiles[name] {
		return o.extra.Open(name)
	}
	return o.base.Open(name)
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		// fs.Sub fails only for an invalid directory name, a programming
		// error in the constants above.
		panic(err)
	}
	return sub
}
