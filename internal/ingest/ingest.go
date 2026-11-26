// Package ingest consumes the versioned fact contract without indexer dependencies.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// BatchSize bounds buffered records while the store transaction protects publication.
const BatchSize = 128

// Stream replaces one repository snapshot only after valid framing and EOF.
func Stream(ctx context.Context, input io.Reader, s store.Store) error {
	_, err := StreamWithWrites(ctx, input, s)
	return err
}

// StreamWithWrites reports the diff after a complete snapshot commits successfully.
func StreamWithWrites(ctx context.Context, input io.Reader, s store.Store) (writes store.Writes, result error) {
	return StreamWithOptions(ctx, input, s, Options{})
}

// Options adds publication checks without changing contract v1 validation.
type Options struct {
	Repository        string
	PreserveFactCache bool
	// BeforeCommit runs after validated EOF, while rollback is still possible.
	BeforeCommit func() error
	// Progress, when set, receives the number of records (stream lines,
	// header included) written to the transaction so far, after each batch
	// of up to BatchSize records. The caller knows the total from the stream it
	// wrote; ingest itself does not.
	Progress func(records int)
}

// StreamWithOptions lets external producers prove successful exit before publication.
func StreamWithOptions(ctx context.Context, input io.Reader, s store.Store, options Options) (writes store.Writes, result error) {
	reader := contract.NewReader(input)
	first, err := profileNext(reader)
	if err != nil {
		return writes, err
	}
	header, ok := first.Record.(*contract.Header)
	if !ok {
		return writes, fmt.Errorf("line 1: invalid stream header")
	}
	if options.Repository != "" && header.Repository != options.Repository {
		return writes, fmt.Errorf("line 1: header repository %q differs from requested repository %q", header.Repository, options.Repository)
	}
	tx, err := s.BeginRun(ctx, *header, store.RunOptions{PreserveFactCache: options.PreserveFactCache})
	if err != nil {
		return writes, fmt.Errorf("line 1: beginning snapshot: %w", err)
	}
	defer tx.Rollback()
	records := 0
	progress := func() {
		if options.Progress != nil {
			options.Progress(records)
		}
	}
	batch := []contract.Event{first}
	edgesStarted := false
	lastLine := first.Line
	flush := func() error {
		for _, event := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			var writeErr error
			switch v := event.Record.(type) {
			case *contract.File:
				if edgesStarted {
					writeErr = fmt.Errorf("%w: file after edges", store.ErrInvalid)
				} else {
					writeErr = tx.UpsertFile(ctx, *v)
				}
			case *contract.Node:
				if edgesStarted {
					writeErr = fmt.Errorf("%w: node after edges", store.ErrInvalid)
				} else {
					writeErr = tx.UpsertNode(ctx, *v)
				}
			case *contract.Edge:
				edgesStarted = true
				writeErr = tx.UpsertEdge(ctx, *v)
			case *contract.Diagnostic:
				writeErr = tx.Diagnostic(ctx, *v)
			}
			if writeErr != nil {
				if !errors.Is(writeErr, store.ErrInvalid) {
					return fmt.Errorf("line %d: %w", event.Line, writeErr)
				}
				event.Diagnostics = append(event.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: "", Severity: "error", Code: "invalid_reference", Message: fmt.Sprintf("line %d: %v", event.Line, writeErr)})
			}
			for _, d := range event.Diagnostics {
				if err := tx.Diagnostic(ctx, d); err != nil {
					return fmt.Errorf("recording line %d diagnostic: %w", event.Line, err)
				}
			}
		}
		records += len(batch)
		batch = batch[:0]
		progress()
		return nil
	}
	for {
		event, err := profileNext(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return writes, err
		}
		batch = append(batch, event)
		lastLine = event.Line
		if len(batch) >= BatchSize {
			if err := flush(); err != nil {
				return writes, err
			}
		}
	}
	if err := flush(); err != nil {
		return writes, err
	}
	if options.BeforeCommit != nil {
		if err := options.BeforeCommit(); err != nil {
			return writes, err
		}
	}
	if err := ctx.Err(); err != nil {
		return writes, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		err = fmt.Errorf("line %d: committing snapshot: %w", lastLine, err)
	}
	return tx.Writes(), err
}

func profileNext(reader *contract.Reader) (contract.Event, error) {
	finish := profileRead()
	event, err := reader.Next()
	if finish != nil {
		finish()
	}
	return event, err
}
