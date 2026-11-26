package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/store"
)

// nameColumnLimit bounds the bytes of one nodes_names index entry. Postgres
// rejects b-tree entries above roughly 2.7 kB; 1536 bytes for repository,
// identity, name and qualified name together leaves room for tuple overhead.
// Rows over the bound keep NULL columns and stay term candidates (ADR 0039).
const nameColumnLimit = 1536

// nameColumns derives the nodes.name and nodes.qualified_name column values
// from a stored payload, using the same decoder settings as graph reads so
// the columns can never disagree with what Nodes decodes. A nil result means
// NULL: the row is then always a term candidate and never excluded by SQL.
func nameColumns(repository, identity, payload string) (any, any) {
	var v struct {
		Name          string `json:"name"`
		QualifiedName string `json:"qualified_name"`
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		// NULL keeps an undecodable payload a candidate for every term, so
		// a term read decodes it and reports the error. A payload that only
		// fails the full contract.Node decode gets columns; ADR 0039 lists
		// that gap.
		return nil, nil
	}
	if strings.ContainsRune(v.Name, 0) || strings.ContainsRune(v.QualifiedName, 0) {
		return nil, nil
	}
	if len(repository)+len(identity)+len(v.Name)+len(v.QualifiedName) > nameColumnLimit {
		return nil, nil
	}
	return v.Name, v.QualifiedName
}

// indexableTerm reports whether a name term can use the SQL prefilter. Other
// terms use the full read, which is what every term used before schema 3:
// U+0000 cannot be bound on Postgres, invalid UTF-8 has no portable SQL
// comparison, and a term longer than the column bound cannot match any
// non-NULL row while it could exceed SQLite's LIKE pattern limit.
func indexableTerm(term string) bool {
	return term != "" && len(term) <= nameColumnLimit && utf8.ValidString(term) && !strings.ContainsRune(term, 0)
}

// suffixPattern builds a LIKE pattern for "ends with term". '!' is the escape
// character so the SQL text needs no backslash, whose literal meaning depends
// on Postgres's standard_conforming_strings. SQLite's LIKE folds ASCII case,
// which only widens the candidate set; Go applies the exact predicate after.
func suffixPattern(term string) string {
	var b strings.Builder
	b.WriteByte('%')
	for i := 0; i < len(term); i++ {
		switch term[i] {
		case '!', '%', '_':
			b.WriteByte('!')
		}
		b.WriteByte(term[i])
	}
	return b.String()
}

// termPrefilter is the SQL superset of the Go name predicate. NULL columns
// are always candidates.
func termPrefilter(term, conditions string, args []any) (string, []any) {
	args = append(args, term, suffixPattern(term))
	t, p := len(args)-1, len(args)
	return conditions + fmt.Sprintf(" AND (qualified_name IS NULL OR name=$%d OR qualified_name=$%d OR qualified_name LIKE $%d ESCAPE '!')", t, t, p), args
}

// containsPrefilter is the SQL superset of the Go Contains predicate: the
// folded text as a LIKE substring of either lower-cased name column. Both
// dialects' lower() fold at least ASCII (SQLite only ASCII, Postgres on the
// "C"-collated columns only ASCII), and store.FoldASCII folds only ASCII,
// so no Go match is excluded. NULL columns are always candidates.
func containsPrefilter(text, conditions string, args []any) (string, []any) {
	args = append(args, "%"+suffixPattern(store.FoldASCII(text))[1:]+"%")
	p := len(args)
	return conditions + fmt.Sprintf(" AND (qualified_name IS NULL OR lower(name) LIKE $%d ESCAPE '!' OR lower(qualified_name) LIKE $%d ESCAPE '!')", p, p), args
}

// nameBackfillBatch is how many rows one backfill read holds, so a large
// store is migrated without loading every payload at once. Store tests mirror
// it to place repository boundaries at a batch edge.
const nameBackfillBatch = 1000

// backfillNames fills the schema 3 name columns for existing rows inside the
// migration transaction. Decoding happens in Go rather than with SQL JSON
// functions so that one decoder defines the stored names (ADR 0039).
func backfillNames(ctx context.Context, tx *sql.Tx) error {
	type row struct{ repository, identity, payload string }
	const batch = nameBackfillBatch
	var lastRepository, lastIdentity string
	first := true
	for {
		q := fmt.Sprintf("SELECT repository,identity,payload FROM nodes ORDER BY repository,identity LIMIT %d", batch)
		args := []any{}
		if !first {
			q = fmt.Sprintf("SELECT repository,identity,payload FROM nodes WHERE repository>$1 OR (repository=$1 AND identity>$2) ORDER BY repository,identity LIMIT %d", batch)
			args = append(args, lastRepository, lastIdentity)
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("reading nodes for name backfill: %w", err)
		}
		var pending []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.repository, &r.identity, &r.payload); err != nil {
				rows.Close()
				return fmt.Errorf("reading node for name backfill: %w", err)
			}
			pending = append(pending, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("reading nodes for name backfill: %w", err)
		}
		for _, r := range pending {
			name, qualified := nameColumns(r.repository, r.identity, r.payload)
			if name == nil {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE nodes SET name=$1,qualified_name=$2 WHERE repository=$3 AND identity=$4", name, qualified, r.repository, r.identity); err != nil {
				return fmt.Errorf("backfilling names for %s: %w", r.identity, err)
			}
		}
		if len(pending) < batch {
			return nil
		}
		last := pending[len(pending)-1]
		lastRepository, lastIdentity, first = last.repository, last.identity, false
	}
}
