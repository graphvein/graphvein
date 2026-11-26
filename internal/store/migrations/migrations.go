// Package migrations bundles numbered, backend-specific database schemas.
package migrations

import "embed"

// Files makes migrations available without a runtime dependency on the checkout.
//
//go:embed sqlite/*.sql postgres/*.sql
var Files embed.FS

// Version prevents an older binary from silently misreading a newer database.
const Version = 6
