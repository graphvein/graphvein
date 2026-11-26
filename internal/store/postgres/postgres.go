// Package postgres supplies team storage through the same graph interface.
package postgres

import (
	"context"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// OpenRead ensures query connections cannot perform writes or migrate schemas.
func OpenRead(ctx context.Context, url string) (store.Store, error) {
	db, err := egress.OpenPostgres(url, map[string]string{"default_transaction_read_only": "on"})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return sqlstore.OpenRead(ctx, db, "postgres")
}

// Open connects only to infrastructure explicitly supplied by the caller.
func Open(ctx context.Context, url string) (store.Store, error) {
	db, err := egress.OpenPostgres(url, nil)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return sqlstore.Open(ctx, db, "postgres")
}
