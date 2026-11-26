package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/store"
)

func TestServeOnRealSocket(t *testing.T) {
	opt := handlerOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := Listen(ctx, "127.0.0.1:0", opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(srv.URL(), "http://127.0.0.1:") || strings.HasSuffix(srv.URL(), ":0/") || !strings.HasSuffix(srv.URL(), "/") {
		t.Fatalf("URL %q", srv.URL())
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
	resp, err := client.Get(srv.URL() + "api/v0/status")
	if err != nil {
		t.Fatal(err)
	}
	var body statusResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || body.Freshness.State != stateNotIndexed {
		t.Fatalf("status %d %+v %v", resp.StatusCode, body, err)
	}

	// The same port is taken now; the error names the address.
	addr := strings.TrimSuffix(strings.TrimPrefix(srv.URL(), "http://"), "/")
	if _, err := Listen(ctx, addr, opt); err == nil || !strings.Contains(err.Error(), addr) || errors.Is(err, egress.ErrBlocked) {
		t.Fatalf("second listener: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5 s")
	}
}

func TestListenRejectsNonLoopback(t *testing.T) {
	opt := handlerOptions(t)
	for _, addr := range []string{"localhost:0", "0.0.0.0:0", ":0"} {
		if _, err := Listen(context.Background(), addr, opt); !errors.Is(err, egress.ErrBlocked) {
			t.Errorf("%s: %v, want egress.ErrBlocked", addr, err)
		}
	}
}

// blockingStore holds FileHashes until the request context ends, standing
// in for a long freshness walk.
type blockingStore struct {
	store.Store
	entered  chan struct{}
	cause    chan error
	returned atomic.Bool
}

func (b *blockingStore) FileHashes(ctx context.Context, repo string) (map[string]string, error) {
	close(b.entered)
	<-ctx.Done()
	b.cause <- context.Cause(ctx)
	b.returned.Store(true)
	return nil, ctx.Err()
}

// TestServeCancelsInFlightRequests stops the server while a status request
// is inside its freshness check: the check observes the cancellation at
// once (not after the grace period), the client gets 503 unavailable, and
// Serve returns only after the handler finished, so closing the store
// afterwards is safe.
func TestServeCancelsInFlightRequests(t *testing.T) {
	opt := handlerOptions(t)
	blocking := &blockingStore{Store: opt.Store, entered: make(chan struct{}), cause: make(chan error, 1)}
	opt.Store = blocking
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := Listen(ctx, "127.0.0.1:0", opt)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	type result struct {
		code int
		body []byte
		err  error
	}
	responses := make(chan result, 1)
	go func() {
		client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
		resp, err := client.Get(srv.URL() + "api/v0/status")
		if err != nil {
			responses <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		responses <- result{resp.StatusCode, body, err}
	}()
	select {
	case <-blocking.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("request did not reach the store")
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return")
	}
	if elapsed := time.Since(start); elapsed >= shutdownGrace {
		t.Fatalf("Serve took %v: the request was not cancelled", elapsed)
	}
	if !blocking.returned.Load() {
		t.Fatal("Serve returned while a handler was still using the store")
	}
	if cause := <-blocking.cause; !errors.Is(cause, errStopping) {
		t.Fatalf("request context cause %v, want errStopping", cause)
	}
	r := <-responses
	if r.err != nil {
		t.Fatal(r.err)
	}
	var env errorEnvelope
	if err := json.Unmarshal(r.body, &env); err != nil || r.code != http.StatusServiceUnavailable || env.Error.Code != "unavailable" {
		t.Fatalf("response %d %s: %v", r.code, r.body, err)
	}
}
