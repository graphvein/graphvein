package egress

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// netAllowed lists package net identifiers that are value types or pure
// functions. Everything else (Dial*, Listen*, Dialer, Resolver, Lookup*,
// File*, Interfaces and anything added later) is reserved for this package.
var netAllowed = setOf("Addr", "Conn", "Listener", "PacketConn", "Error", "OpError",
	"AddrError", "DNSError", "ParseError", "InvalidAddrError", "UnknownNetworkError",
	"IP", "IPAddr", "IPMask", "IPNet", "TCPAddr", "UDPAddr", "UnixAddr", "HardwareAddr",
	"ErrClosed", "ParseIP", "ParseCIDR", "ParseMAC", "IPv4", "IPv4Mask", "CIDRMask",
	"SplitHostPort", "JoinHostPort", "Pipe")

var tlsBlocked = setOf("Dial", "DialWithDialer", "Dialer", "Listen")

var httpBlocked = setOf("Get", "Head", "Post", "PostForm", "DefaultClient",
	"DefaultTransport", "Client", "Transport", "ListenAndServe", "ListenAndServeTLS")

// syscallFiles may import syscall or golang.org/x/sys, with the reason.
var syscallFiles = map[string]string{
	"cmd/profile/memory_windows.go":      "process memory and CPU queries for the developer profiler",
	"internal/workspace/lock_unix.go":    "flock(2) on the workspace registry's lock file (ADR 0087), no network",
	"internal/workspace/lock_windows.go": "LockFileEx on the workspace registry's lock file (ADR 0087), no network",
}

// execPackages may import os/exec. Subprocesses are never under in-process
// network control, so every new one must be reviewed and listed here.
var execPackages = map[string]string{
	"internal/externalindex": "runs the user's external indexer (ADR 0010), outside network controls",
	"internal/scan":          "runs local git rev-parse to read the object format, and ls-tree for the run's git state",
	"internal/gitref":        "runs local git rev-parse, merge-base, ls-tree and cat-file with transports disabled (ADR 0056)",
	"cmd/profile":            "developer profiler runs graphvein and reg.exe",
	"internal/desktop":       "opens graphvein's own loopback URL with the system URL handler (rundll32, open, xdg-open); no network",
	"scripts/notices":        "licence notice generator runs go list with GOPROXY=off (ADR 0080); not linked into the binary",
}

// mcpSDK is the MCP SDK (ADR 0082). First-party code may import only its
// mcp package, and only for the stdio server API in mcpAllowed: every
// client, HTTP, SSE and command transport, OAuth and the in-memory pair
// are rejected by name, in test files too, because the SDK's oauthex
// package captures http.DefaultTransport at init, which the runtime
// tripwires cannot see.
const mcpSDK = "github.com/modelcontextprotocol/go-sdk"

var mcpAllowed = setOf("NewServer", "Server", "ServerOptions", "ServerCapabilities", "Implementation",
	"AddTool", "Tool", "ToolAnnotations", "CallToolRequest", "CallToolResult", "Content", "TextContent",
	"IOTransport", "StdioTransport", "Transport")

// cgoPrefix is the only place cgo is approved (ADR 0002: the native
// comparison in spike/parsing/internal/parse behind the native && cgo build
// tags). spike/parsing is its own module, so the walk never reaches it; the
// exemption is kept so the rule states the ADR exactly.
const cgoPrefix = "spike/parsing/internal/parse/"

// processStart lists selectors that start a process without os/exec; they
// fall under the same subprocess allow-list.
var processStart = map[string]map[string]bool{
	"os":      setOf("StartProcess"),
	"syscall": setOf("ForkExec", "StartProcess", "Exec"),
}

func setOf(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// scanSource reports boundary violations in one file. rel is the slash path
// relative to the module root; it decides exemptions.
func scanSource(rel string, src []byte) ([]string, error) {
	if strings.HasPrefix(rel, "internal/egress/") {
		return nil, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", rel, err)
	}
	isTest := strings.HasSuffix(rel, "_test.go")
	dir := filepath.ToSlash(filepath.Dir(rel))
	var found []string
	at := func(pos token.Pos, format string, args ...any) {
		found = append(found, fmt.Sprintf("%s:%d: %s", rel, fset.Position(pos).Line, fmt.Sprintf(format, args...)))
	}
	local := map[string]string{} // local name -> import path
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch {
		case strings.HasPrefix(path, mcpSDK+"/") && path != mcpSDK+"/mcp", path == mcpSDK,
			path == "golang.org/x/oauth2" || strings.HasPrefix(path, "golang.org/x/oauth2/"):
			at(imp.Pos(), "imports %s; only the MCP SDK's stdio server API (package mcp) is allowed (ADR 0082)", path)
		case name == "." && path == mcpSDK+"/mcp":
			at(imp.Pos(), "dot import of %s", path)
		case path == "github.com/jackc/pgx/v5" || strings.HasPrefix(path, "github.com/jackc/pgx/v5/"):
			at(imp.Pos(), "imports %s; use egress.OpenPostgres", path)
		case name == "." && (path == "net" || path == "net/http" || path == "crypto/tls" || path == "database/sql"):
			at(imp.Pos(), "dot import of %s", path)
		case path == "C":
			if !strings.HasPrefix(rel, cgoPrefix) {
				at(imp.Pos(), `import "C" (cgo) outside the ADR 0002 package`)
			}
		case isTest:
		case path == "net/http/httputil" || path == "net/http/httptest" || path == "net/smtp" ||
			path == "net/textproto" || path == "net/rpc" || strings.HasPrefix(path, "net/rpc/") ||
			path == "log/syslog" || path == "plugin" ||
			path == "golang.org/x/net" || strings.HasPrefix(path, "golang.org/x/net/"):
			// These dial, listen or load code themselves (textproto.Dial,
			// jsonrpc.Dial, syslog.Dial, httptest.NewServer, plugin.Open).
			at(imp.Pos(), "imports %s", path)
		case path == "syscall" || path == "golang.org/x/sys" || strings.HasPrefix(path, "golang.org/x/sys/"):
			if syscallFiles[rel] == "" {
				at(imp.Pos(), "imports %s outside the syscall allow-list", path)
			}
		case path == "os/exec":
			if execPackages[dir] == "" {
				at(imp.Pos(), "imports os/exec outside the subprocess allow-list")
			}
		}
		local[name] = path
	}
	// sql.Open calls with the literal "sqlite" driver are the only allowed
	// uses; remember those selectors before the general walk.
	sqliteOpen := map[*ast.SelectorExpr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == `"sqlite"` {
				sqliteOpen[sel] = true
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if !isTest && (name == "ListenAndServe" || name == "ListenAndServeTLS") {
			at(sel.Pos(), "%s; serve an egress.ListenLoopback listener instead", name)
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if !isTest && processStart[local[x.Name]][name] && execPackages[dir] == "" {
			at(sel.Pos(), "%s.%s outside the subprocess allow-list", x.Name, name)
			return true
		}
		switch local[x.Name] {
		case mcpSDK + "/mcp":
			if !mcpAllowed[name] {
				at(sel.Pos(), "mcp.%s: only the MCP SDK's stdio server API is allowed (ADR 0082)", name)
			}
		case "database/sql":
			if name == "OpenDB" || (name == "Open" && !sqliteOpen[sel]) {
				at(sel.Pos(), "sql.%s outside egress; use egress.OpenPostgres", name)
			}
		case "net":
			if !isTest && !netAllowed[name] {
				at(sel.Pos(), "net.%s outside egress", name)
			}
		case "crypto/tls":
			if !isTest && tlsBlocked[name] {
				at(sel.Pos(), "tls.%s outside egress", name)
			}
		case "net/http":
			if !isTest && httpBlocked[name] {
				at(sel.Pos(), "http.%s outside egress; use Policy.HTTPClient", name)
			}
		}
		return true
	})
	return found, nil
}

// moduleRoot finds the directory holding go.mod above the test's directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// TestNetworkBoundary enforces that only internal/egress opens connections.
// It walks the files `go test ./...` would see, for every build tag and OS.
func TestNetworkBoundary(t *testing.T) {
	root := moduleRoot(t)
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		found, err := scanSource(filepath.ToSlash(rel), src)
		if err != nil {
			return err
		}
		for _, f := range found {
			t.Error(f)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 50 {
		t.Fatalf("scanned only %d files; is the module root right?", files)
	}
}

// TestBoundaryRules proves each rule fires and the allowed cases pass.
func TestBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		path, src  string
		violations int
	}{
		{"internal/x/a.go", `package x; import "net"; var _, _ = net.Dial("tcp", "h:1")`, 1},
		{"internal/x/a.go", `package x; import "net"; var d net.Dialer`, 1},
		{"internal/x/a.go", `package x; import "net"; var r = net.DefaultResolver`, 1},
		{"internal/x/a.go", `package x; import "net"; var _ = net.Interfaces`, 1},
		{"internal/x/a.go", `package x; import n "net"; var _, _ = n.Listen("tcp", ":0")`, 1},
		{"internal/x/a.go", `package x; import "net"; var _ = net.ParseIP("1.2.3.4"); var _ net.Conn`, 0},
		{"internal/x/a.go", `package x; import "crypto/tls"; var _, _ = tls.Dial("tcp", "h:1", nil)`, 1},
		{"internal/x/a.go", `package x; import "crypto/tls"; var _ tls.Config`, 0},
		{"internal/x/a.go", `package x; import "net/http"; var _, _ = http.Get("http://x")`, 1},
		{"internal/x/a.go", `package x; import "net/http"; var _ = &http.Client{}`, 1},
		{"internal/x/a.go", `package x; import "net/http"; var _ = http.DefaultTransport`, 1},
		{"internal/x/a.go", `package x; import "net/http"; func f(s *http.Server) { s.ListenAndServe() }`, 1},
		{"internal/x/a.go", `package x; import "net/http"; func f(s *http.Server) { s.ListenAndServeTLS("", "") }`, 1},
		{"internal/x/a.go", `package x; import "net/http"; var _ http.Handler; var _ = http.StatusOK`, 0},
		{"internal/x/a.go", `package x; import _ "net/http/httputil"`, 1},
		{"internal/x/a.go", `package x; import _ "net/rpc"`, 1},
		{"internal/x/a.go", `package x; import _ "net/smtp"`, 1},
		{"internal/x/a.go", `package x; import _ "net/rpc/jsonrpc"`, 1},
		{"internal/x/a.go", `package x; import _ "net/textproto"`, 1},
		{"internal/x/a.go", `package x; import _ "log/syslog"`, 1},
		{"internal/x/a.go", `package x; import _ "net/http/httptest"`, 1},
		{"internal/x/a.go", `package x; import _ "net/netip"; import _ "net/url"; import _ "net/mail"; import _ "net/http/httptrace"`, 0},
		{"internal/x/a.go", `package x; import "os"; var _, _ = os.StartProcess("sh", nil, nil)`, 1},
		{"internal/x/a.go", `package x; import "os"; var _ = os.Getpid()`, 0},
		{"cmd/profile/main.go", `package main; import "os"; var _, _ = os.StartProcess("sh", nil, nil)`, 0},
		{"cmd/profile/memory_windows.go", `package main; import "syscall"; var _, _ = syscall.ForkExec("sh", nil, nil)`, 0},
		{"internal/x/a.go", `package x; import "syscall"; var _, _ = syscall.ForkExec("sh", nil, nil)`, 2},
		{"internal/x/a.go", `package x; import "C"`, 1},
		{"internal/x/a_test.go", `package x; import "C"`, 1},
		{"spike/parsing/internal/parse/native.go", `package parse; import "C"`, 0},
		{"internal/x/a.go", `package x; import _ "golang.org/x/net/proxy"`, 1},
		{"internal/x/a.go", `package x; import _ "plugin"`, 1},
		{"internal/x/a.go", `package x; import _ "syscall"`, 1},
		{"internal/x/a.go", `package x; import _ "golang.org/x/sys/unix"`, 1},
		{"cmd/profile/memory_windows.go", `package main; import _ "syscall"`, 0},
		{"internal/x/a.go", `package x; import _ "os/exec"`, 1},
		{"internal/scan/scan.go", `package scan; import _ "os/exec"`, 0},
		{"internal/externalindex/external.go", `package externalindex; import _ "os/exec"`, 0},
		{"cmd/profile/main.go", `package main; import _ "os/exec"`, 0},
		{"internal/x/a.go", `package x; import _ "github.com/jackc/pgx/v5"`, 1},
		{"internal/x/a_test.go", `package x; import _ "github.com/jackc/pgx/v5/stdlib"`, 1},
		{"internal/x/a.go", `package x; import "database/sql"; var _, _ = sql.Open("pgx", "u")`, 1},
		{"internal/x/a_test.go", `package x; import "database/sql"; var _, _ = sql.Open("pgx", "u")`, 1},
		{"internal/x/a.go", `package x; import "database/sql"; var d = "sqlite"; var _, _ = sql.Open(d, "u")`, 1},
		{"internal/x/a.go", `package x; import "database/sql"; var _ = sql.Open`, 1},
		{"internal/x/a_test.go", `package x; import "database/sql"; var _ = sql.OpenDB(nil)`, 1},
		{"internal/x/a.go", `package x; import "database/sql"; var _, _ = sql.Open("sqlite", "p")`, 0},
		{"internal/x/a_test.go", `package x; import . "net"; var _ = IP{}`, 1},
		{"internal/x/a.go", `package x; import . "database/sql"`, 1},
		// The MCP SDK: the stdio server API only (ADR 0082).
		{"internal/mcp/a.go", `package mcp; import sdk "github.com/modelcontextprotocol/go-sdk/mcp"; var _ = sdk.NewServer(nil, nil); var _ sdk.StdioTransport; var _ sdk.IOTransport`, 0},
		{"internal/mcp/a.go", `package mcp; import "github.com/modelcontextprotocol/go-sdk/mcp"; var _ = mcp.NewClient(nil, nil)`, 1},
		{"internal/mcp/a.go", `package mcp; import "github.com/modelcontextprotocol/go-sdk/mcp"; var _ mcp.CommandTransport`, 1},
		{"internal/mcp/a.go", `package mcp; import "github.com/modelcontextprotocol/go-sdk/mcp"; var _ = mcp.NewStreamableHTTPHandler`, 1},
		{"internal/mcp/a.go", `package mcp; import "github.com/modelcontextprotocol/go-sdk/mcp"; var _ mcp.SSEHandler; var _ mcp.StreamableClientTransport`, 2},
		{"internal/mcp/a_test.go", `package mcp; import "github.com/modelcontextprotocol/go-sdk/mcp"; var _, _ = mcp.NewInMemoryTransports()`, 1},
		{"internal/mcp/a.go", `package mcp; import _ "github.com/modelcontextprotocol/go-sdk/auth"`, 1},
		{"internal/mcp/a.go", `package mcp; import _ "github.com/modelcontextprotocol/go-sdk/oauthex"`, 1},
		{"internal/mcp/a.go", `package mcp; import _ "golang.org/x/oauth2"`, 1},
		// Test files may use loopback test servers and resolver tripwires.
		{"internal/x/a_test.go", `package x; import ("net"; "net/http"; _ "os/exec"; _ "syscall"); var _, _ = net.Listen("tcp", "127.0.0.1:0"); var _ = http.DefaultTransport`, 0},
		// The boundary package itself is exempt.
		{"internal/egress/policy.go", `package egress; import ("net"; _ "github.com/jackc/pgx/v5"); var _ net.Dialer`, 0},
		{"internal/egress/sub/a.go", `package sub; import "net"; var _ net.Dialer`, 0},
	} {
		found, err := scanSource(tc.path, []byte(tc.src))
		if err != nil {
			t.Fatal(tc.src, err)
		}
		if len(found) != tc.violations {
			t.Errorf("%s: %s: got %d violations %v, want %d", tc.path, tc.src, len(found), found, tc.violations)
		}
	}
}
