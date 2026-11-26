// Package contractschema bundles the published schemas so validation is offline.
package contractschema

import "embed"

// Files embeds the authoritative specification rather than maintaining a copy.
//
//go:embed *.schema.json
var Files embed.FS
