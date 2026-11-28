package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

// Counts is what a copied store is checked against before the original is
// deleted (graphvein store move, ADR 0093): the id of every stored index
// run and the row count of every table.
type Counts struct {
	RunIDs []string
	Tables map[string]int64
}

// Equal reports whether c and o list the same runs and the same row count
// for every table.
func (c Counts) Equal(o Counts) bool {
	if len(c.RunIDs) != len(o.RunIDs) || len(c.Tables) != len(o.Tables) {
		return false
	}
	for i := range c.RunIDs {
		if c.RunIDs[i] != o.RunIDs[i] {
			return false
		}
	}
	for name, n := range c.Tables {
		if m, ok := o.Tables[name]; !ok || m != n {
			return false
		}
	}
	return true
}

// CopyInto writes a consistent copy of the store at src to the new file
// dst with SQLite's VACUUM INTO, which reads the last committed state,
// including what is still in src's -wal file, and leaves src unchanged.
// dst must not exist: VACUUM INTO never overwrites. The copy is a
// rollback-journal file with no sidecar files; the first writer that opens
// it puts it back into WAL mode (ADR 0049). The caller holds the index
// lock of src, so no run commits while it is copied.
func CopyInto(ctx context.Context, src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("copying the store to %s: the file already exists", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("copying the store to %s: %w", dst, err)
	}
	db, err := openPlainRead(src)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("copying the store %s to %s: %w", src, dst, err)
	}
	return nil
}

// ReadCounts reads the Counts of the store at p without changing it.
func ReadCounts(ctx context.Context, p string) (Counts, error) {
	db, err := openPlainRead(p)
	if err != nil {
		return Counts{}, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return Counts{}, fmt.Errorf("listing the tables of %s: %w", p, err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return Counts{}, fmt.Errorf("listing the tables of %s: %w", p, err)
		}
		tables = append(tables, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return Counts{}, fmt.Errorf("listing the tables of %s: %w", p, err)
	}
	c := Counts{Tables: map[string]int64{}}
	for _, name := range tables {
		var n int64
		// The name comes from sqlite_schema; quoting keeps any
		// spelling one identifier.
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "`+quoteIdent(name)+`"`).Scan(&n); err != nil {
			return Counts{}, fmt.Errorf("counting the rows of %s in %s: %w", name, p, err)
		}
		c.Tables[name] = n
	}
	if _, ok := c.Tables["index_runs"]; ok {
		ids, err := db.QueryContext(ctx, "SELECT id FROM index_runs")
		if err != nil {
			return Counts{}, fmt.Errorf("reading the run ids of %s: %w", p, err)
		}
		for ids.Next() {
			var id string
			if err := ids.Scan(&id); err != nil {
				ids.Close()
				return Counts{}, fmt.Errorf("reading the run ids of %s: %w", p, err)
			}
			c.RunIDs = append(c.RunIDs, id)
		}
		if err := errors.Join(ids.Err(), ids.Close()); err != nil {
			return Counts{}, fmt.Errorf("reading the run ids of %s: %w", p, err)
		}
		sort.Strings(c.RunIDs)
	}
	return c, nil
}

// openPlainRead opens p read-only without the store's schema checks, for
// the copy and its check; a missing file is an error, never created.
func openPlainRead(p string) (*sql.DB, error) {
	if _, err := os.Stat(p); err != nil {
		return nil, fmt.Errorf("opening the store %s: %w", p, err)
	}
	dsn, err := readDSN(p, writeBusyTimeout)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the store %s: %w", p, err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func quoteIdent(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		if name[i] == '"' {
			out = append(out, '"')
		}
		out = append(out, name[i])
	}
	return string(out)
}
