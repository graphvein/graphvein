// Package mcp is Graphvein's read-only MCP server (ADR 0082, T030): tools
// a coding agent calls over stdio, each a thin mapping of an existing read
// path (the query package for find, callers and impact, the HTTP API
// views through server.Reader for the rest) into one response envelope
// that carries the same honesty as the UI: index freshness on every
// answer, the caps that bound, unknowns, assumptions, the labels in use
// and paging that always says what was not shown. The claim contract is
// docs/contract/claims/mcp-server.md; claimRows keeps it 1:1.
//
// The server speaks stdio only. It never listens and never dials: the SDK
// is used through NewServer, AddTool and the newline-delimited I/O
// transport over standard input and output, and the egress boundary test
// rejects every other SDK transport and client identifier (ADR 0082).
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store"
)

// Encoding says how an envelope is put into a tool result (D5). The JSON
// is built in one place (encode), so a client that feeds both copies to
// its model can be switched to structured-only without touching a tool.
type Encoding int

const (
	// EncodingBoth returns structuredContent and the same minified JSON
	// as the one text block (the MCP spec's advice for older clients).
	EncodingBoth Encoding = iota
	// EncodingStructured returns structuredContent and a one-line text
	// summary that points to it (first bound or error sentence first).
	EncodingStructured
)

// Options configures one MCP server: a repository (Root, Repository) or,
// with Project, a project whose member views take a repository argument.
type Options struct {
	// Root is the absolute repository directory freshness scans.
	Root string
	// Repository is the logical repository ID.
	Repository string
	// Project, when set, serves a project (graphvein mcp --project).
	Project *config.Project
	// Store is an open, read-only store; the caller closes it.
	Store store.Store
	// Version is the binary's version, reported to the client.
	Version string
	// Now returns the current time; nil means time.Now. Tests fix it so
	// checked_at is deterministic.
	Now func() time.Time
	// Encoding is the result encoding (D5); the zero value is both.
	Encoding Encoding
	// FreshnessReuse is how long one freshness check serves the calls
	// after it; 0 means the package default, negative checks every call.
	FreshnessReuse time.Duration
	// Logger receives failures; nil discards. It must not write to the
	// stdio transport's stdout.
	Logger *slog.Logger
}

// Server is one MCP server over one repository or project.
type Server struct {
	opt    Options
	reader *server.Reader
	sdk    *sdk.Server

	// freshMu guards the last freshness check (freshView).
	freshMu  sync.Mutex
	freshRaw json.RawMessage
	freshAt  time.Time
	// freshRuns names the latest runs the last check saw (latestRuns).
	freshRuns string
}

// New builds the server and registers its tools. Close it when done.
func New(opt Options) *Server {
	logger := opt.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	opt.Logger = logger
	so := server.Options{Root: opt.Root, Repository: opt.Repository, Version: opt.Version, Store: opt.Store, Now: opt.Now, Logger: logger}
	if opt.Project != nil {
		so.Project = &server.ProjectOptions{Project: *opt.Project}
	}
	s := &Server{opt: opt, reader: server.NewReader(so)}
	s.sdk = sdk.NewServer(&sdk.Implementation{Name: "graphvein", Version: opt.Version}, &sdk.ServerOptions{
		Instructions: instructions,
		Logger:       logger,
		// No logging capability: the server reports through results only.
		Capabilities: &sdk.ServerCapabilities{},
	})
	for _, t := range tools {
		if opt.Project != nil && !t.project {
			continue
		}
		s.add(t)
	}
	return s
}

// instructions is what a client may show its model about the server. It
// names the questions the tools answer and asks the agent to look for the
// tools first, because a client that defers MCP tools behind a tool search
// (Codex 0.171.0 does) otherwise rarely finds them. It stays short: a
// client sends it in every session. It states nothing the tools and the
// envelope do not.
const instructions = "Graphvein answers questions about this codebase from a stored index: routes and their handlers, the callers of a route or function, what a change or a node reaches, tables and the code that reads or writes them, and unresolved calls. Every answer carries the index freshness, its labels, unknowns, assumptions and the caps that bound it (an empty result with a bound cap means unknown beyond the cap, not none). Absence of a result is never proof of absence. For such questions about this repository, search for and use Graphvein's tools before reading files."

// Close releases the views' background state.
func (s *Server) Close() { s.reader.Close() }

// Serve runs one session over in and out (newline-delimited JSON-RPC, the
// stdio transport of MCP) until the client closes in or ctx ends.
// graphvein mcp passes its standard input and output; nothing else is a
// transport of this server (no listener, no client).
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return s.sdk.Run(ctx, &sdk.IOTransport{Reader: io.NopCloser(in), Writer: nopWriteCloser{out}})
}

// nopWriteCloser keeps the caller's output open when the session ends,
// as the SDK's StdioTransport does with stdout.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// tool is one registered tool. run fills the envelope; an error is a
// refusal (*toolError) or a failed read.
type tool struct {
	name        string
	title       string
	description string
	// project is true when the tool also answers in project mode.
	project bool
	// register adds the tool to the SDK server (toolFor).
	register func(s *Server, t tool)
}

// add registers t with the SDK.
func (s *Server) add(t tool) { t.register(s, t) }

// outputSchema is the envelope's schema: every tool returns one.
var outputSchema = map[string]any{
	"type":     "object",
	"required": []string{"tool", "envelope", "freshness", "request", "summary", "results", "unknowns", "assumptions", "bounds", "page", "labels", "omitted"},
}

// toolFor builds a registration for a tool whose arguments are In: the
// SDK infers the input schema from In and validates arguments; call fills
// the envelope.
func toolFor[In any](call func(ctx context.Context, s *Server, in In, e *Envelope) error) func(s *Server, t tool) {
	return func(s *Server, t tool) {
		readOnly := true
		notDestructive, closedWorld := false, false
		sdk.AddTool(s.sdk, &sdk.Tool{
			Name: t.name, Title: t.title, Description: t.description, OutputSchema: outputSchema,
			Annotations: &sdk.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: true, DestructiveHint: &notDestructive, OpenWorldHint: &closedWorld},
		}, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
			e := newEnvelope(t.name)
			tagRow("MC-19")
			if s.opt.Project != nil {
				e.Project = s.opt.Project.Name
			} else {
				e.Repository = s.opt.Repository
			}
			// status always checks: its question is whether the index is
			// current (R1).
			err := s.freshness(ctx, e, t.name == "status")
			if err == nil {
				err = call(ctx, s, in, e)
			}
			if err == nil {
				err = s.recheck(ctx, e)
			}
			if err == nil {
				err = s.fillAssumptions(ctx, e)
			}
			return s.encode(e, err), nil, nil
		})
	}
}

// encode turns an envelope and the tool's error into the tool result
// (D5). This is the one place the encoding is decided.
func (s *Server) encode(e *Envelope, err error) *sdk.CallToolResult {
	if err != nil {
		e.Error = s.refusal(err)
		tagRow("MC-8")
	}
	e.finish()
	raw, merr := json.Marshal(e)
	if merr != nil {
		// Only a value json cannot encode reaches here: a defect, still
		// answered as an error result with freshness kept.
		raw, _ = json.Marshal(map[string]any{"tool": e.Tool, "envelope": EnvelopeVersion, "freshness": e.Freshness, "error": ToolError{Code: "store_error", Message: merr.Error()}})
	}
	res := &sdk.CallToolResult{StructuredContent: json.RawMessage(raw), IsError: e.Error != nil}
	switch s.opt.Encoding {
	case EncodingStructured:
		res.Content = []sdk.Content{&sdk.TextContent{Text: summaryLine(e)}}
	default:
		res.Content = []sdk.Content{&sdk.TextContent{Text: string(raw)}}
	}
	return res
}

// summaryLine is the text of EncodingStructured: the error or the first
// bound first, so a reader that skims the text still sees a cut.
func summaryLine(e *Envelope) string {
	switch {
	case e.Error != nil:
		return e.Tool + ": " + e.Error.Code + ": " + e.Error.Message + " (full answer in structuredContent)"
	case len(e.Bounds) > 0:
		return e.Tool + ": " + e.Bounds[0].Text + " (full answer in structuredContent)"
	}
	return fmt.Sprintf("%s: %d results, freshness %s (full answer in structuredContent)", e.Tool, len(e.Results), e.Freshness.State)
}

// refusal maps an error to the envelope's error (MC-8).
func (s *Server) refusal(err error) *ToolError {
	var te *toolError
	if errors.As(err, &te) {
		return &ToolError{Code: te.code, Message: te.message, Candidates: te.candidates}
	}
	var re *server.ReadError
	if errors.As(err, &re) {
		return &ToolError{Code: re.Code, Message: re.Message}
	}
	s.opt.Logger.Error("tool failed", "error", err)
	return &ToolError{Code: "store_error", Message: err.Error()}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// fillAssumptions turns the assumption IDs a response's facts rest on
// into envelope assumptions with the catalogue's text (the schema
// assumptions view: migration and model rows, TA-V table access). An ID
// the catalogue does not hold is passed on with that said (MC-D).
func (s *Server) fillAssumptions(ctx context.Context, e *Envelope) error {
	if len(e.assumeSet) == 0 {
		return nil
	}
	q := url.Values{}
	if r, ok := e.Request["repository"].(string); ok && s.opt.Project != nil {
		q.Set("repository", r)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/schema/assumptions", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	texts := map[string]string{}
	for _, a := range listOf(v["assumptions"]) {
		m, _ := a.(map[string]any)
		if id := str(m["id"]); id != "" && texts[id] == "" {
			texts[id] = str(m["title"]) + ": " + str(m["text"])
		}
	}
	for _, id := range sortedStrings(e.assumeSet) {
		text, ok := texts[id]
		if !ok {
			e.notModelled("assumption "+id, id)
			text = "not in the assumption catalogue: no meaning claimed"
		}
		e.Assumptions = append(e.Assumptions, Assumption{ID: id, Text: text, Row: e.assumeSet[id]})
	}
	return nil
}
