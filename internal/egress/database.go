package egress

import (
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres opens a database/sql handle whose every connection, including
// pooled reconnects and cancel requests, dials only the hosts named in url.
// The allow-list is built from the parsed configuration (host, port and every
// fallback that PG* variables or sslmode add), never from the raw string.
// runtimeParams are set on every connection, for example a read-only
// transaction default or a test search_path.
//
// This is the only place the pgx driver is used, so nothing else can open a
// Postgres connection that bypasses the dialer.
func OpenPostgres(url string, runtimeParams map[string]string) (*sql.DB, error) {
	config, _, err := postgresConfig(url, runtimeParams)
	if err != nil {
		return nil, err
	}
	return stdlib.OpenDB(*config), nil
}

// postgresConfig is OpenPostgres without the database/sql wrapper, so tests
// can connect through exactly the hooks OpenPostgres installs.
func postgresConfig(url string, runtimeParams map[string]string) (*pgx.ConnConfig, *Policy, error) {
	config, policy, err := parsePostgres(url)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range runtimeParams {
		config.RuntimeParams[k] = v
	}
	// Set last so no earlier step can replace them.
	config.DialFunc = policy.DialContext
	config.LookupFunc = policy.LookupHost
	return config, policy, nil
}

// parsePostgres derives the database destinations from the parsed
// configuration, so tests can inspect the policy OpenPostgres installs.
func parsePostgres(url string) (*pgx.ConnConfig, *Policy, error) {
	config, err := pgx.ParseConfig(url)
	if err != nil {
		// The URL may contain a password; never echo it or the parse error.
		return nil, nil, fmt.Errorf("invalid Postgres configuration")
	}
	destinations := []Destination{{Host: config.Host, Port: config.Port, Purpose: Database}}
	for _, fb := range config.Fallbacks {
		destinations = append(destinations, Destination{Host: fb.Host, Port: fb.Port, Purpose: Database})
	}
	policy, err := NewPolicy(Offline, destinations...)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Postgres configuration")
	}
	return config, policy, nil
}
