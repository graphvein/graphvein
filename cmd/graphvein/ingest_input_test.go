package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// pipeReader hides every method but Read, as a pipe offers no Seek.
type pipeReader struct{ r io.Reader }

func (p pipeReader) Read(b []byte) (int, error) { return p.r.Read(b) }

// TestIngestPipedStream: graphvein ingest accepts a non-seekable stream (a
// pipe, /dev/stdin) on both backends, as it did before the index lock; on
// Postgres the header that names the lock is read from the same bytes.
func TestIngestPipedStream(t *testing.T) {
	data, err := os.ReadFile("../../testdata/contract/valid.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	lockBackends(t, func(t *testing.T, _ store.IndexLocker, _ string, runs func() int) {
		// The store is new: it exists only after the first ingest.
		if err := ingestInput("stdin", pipeReader{iotest.OneByteReader(bytes.NewReader(data))}); err != nil {
			t.Fatal(err)
		}
		if after := runs(); after != 1 {
			t.Fatalf("%d runs after a piped ingest, want 1", after)
		}
		if runtime.GOOS == "windows" {
			return // no /dev/fd; the reader path above covers the stream
		}
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		go func() {
			_, _ = w.Write(data)
			w.Close()
		}()
		var out, errOut bytes.Buffer
		if code := run([]string{"ingest", "/dev/fd/" + strconv.Itoa(int(r.Fd()))}, &out, &errOut); code != 0 {
			t.Fatalf("ingest from a pipe: exit %d, stderr %q", code, errOut.String())
		}
		if after := runs(); after != 2 {
			t.Fatalf("%d runs after ingest from a pipe, want 2", after)
		}
	})
}

// TestReadStreamHeaderKeepsBytes: the reader readStreamHeader returns
// yields exactly the input, header line included, also when the header is
// longer than the buffer and the input is a one-byte pipe; so the stream
// ingest writes is the one whose header named the lock.
func TestReadStreamHeaderKeepsBytes(t *testing.T) {
	valid, err := os.ReadFile("../../testdata/contract/valid.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	long := bytes.Replace(valid, []byte(`"commit":"abc123"`), []byte(`"commit":"`+strings.Repeat("a", 20000)+`"`), 1)
	for name, data := range map[string][]byte{"short header": valid, "long header": long} {
		header, rest, err := readStreamHeader(pipeReader{iotest.OneByteReader(bytes.NewReader(data))})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if header.Repository != "fixture" {
			t.Fatalf("%s: repository %q", name, header.Repository)
		}
		got, err := io.ReadAll(rest)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s: stream differs from the input (%d bytes, want %d): %v", name, len(got), len(data), err)
		}
	}
	for _, bad := range []string{"", "{\"type\":\"header\"", "{\"type\":\"file\"}\n"} {
		if _, _, err := readStreamHeader(strings.NewReader(bad)); err == nil {
			t.Fatalf("%q: header accepted", bad)
		}
	}
}

// TestIngestLockedRefusesOtherRepository: a stream whose header names a
// repository other than the locked one fails before anything is written.
func TestIngestLockedRefusesOtherRepository(t *testing.T) {
	data, err := os.ReadFile("../../testdata/contract/valid.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	lockBackends(t, func(t *testing.T, _ store.IndexLocker, _ string, runs func() int) {
		ctx := context.Background()
		var s store.Store
		if url := os.Getenv("GRAPHVEIN_POSTGRES_URL"); url != "" {
			s, err = postgres.Open(ctx, url)
		} else {
			s, err = sqlite.Open(ctx, os.Getenv("GRAPHVEIN_SQLITE_PATH"))
		}
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		before := runs()
		err := ingestLocked(ctx, s, "other", bytes.NewReader(data))
		if err == nil || !strings.Contains(err.Error(), `differs from requested repository "other"`) {
			t.Fatalf("mismatched repository: %v", err)
		}
		if after := runs(); after != before {
			t.Fatalf("store changed: %d runs, had %d", after, before)
		}
		other, err := s.Runs(ctx, "other")
		if err != nil || len(other) != 0 {
			t.Fatalf("runs of other: %v %v", other, err)
		}
	})
}
