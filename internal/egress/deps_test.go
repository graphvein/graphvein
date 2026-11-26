package egress

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// networkDeps is the reviewed set of non-standard packages linked into
// cmd/graphvein that import a networking or subprocess package. A new entry
// fails TestDependencySurface until someone checks that it cannot open a
// connection outside egress and adds it here with the reason.
var networkDeps = []struct {
	pkg, reason string
	goos        []string // nil means every checked GOOS
}{
	{"github.com/graphvein/graphvein/internal/egress", "the boundary itself", nil},
	{"github.com/graphvein/graphvein/internal/externalindex", "os/exec: runs the user's external indexer (ADR 0010)", nil},
	{"github.com/graphvein/graphvein/internal/scan", "os/exec: local git rev-parse and ls-tree", nil},
	{"github.com/graphvein/graphvein/internal/gitref", "os/exec: local git with protocol.allow=never and GIT_NO_LAZY_FETCH=1 (ADR 0056)", nil},
	{"github.com/graphvein/graphvein/internal/server", "net/http: serves on an egress.ListenLoopback listener; no client", nil},
	{"github.com/jackc/pgx/v5/pgconn", "net, crypto/tls: dials only through Config.DialFunc/LookupFunc, which egress installs", nil},
	{"github.com/jackc/pgx/v5/pgtype", "net: value types for inet/cidr columns only", nil},
	{"github.com/tetratelabs/wazero", "net: socket support only with an explicit experimental sock config, never configured", nil},
	{"github.com/tetratelabs/wazero/internal/sock", "net: as above", nil},
	{"github.com/tetratelabs/wazero/internal/sys", "net: as above", nil},
	{"github.com/tetratelabs/wazero/internal/sysfs", "net: as above", nil},
	{"github.com/google/uuid", "net: reads local interfaces (net.Interfaces), no connection", []string{"linux", "darwin"}},
	{"golang.org/x/sys/windows", "net: net.IP conversion of socket address structs, no connection", []string{"windows"}},
	{"modernc.org/libc", "os/exec: system() emulation, not reached by the SQLite driver", nil},
	// The MCP SDK (ADR 0082): graphvein mcp uses only mcp.NewServer,
	// mcp.AddTool and the newline-delimited I/O transport over stdio. The
	// client transports (CommandTransport, streamable HTTP, SSE) and OAuth
	// are linked but never reached; TestNetworkBoundary rejects their
	// identifiers in first-party code and the mcp runtime test checks a
	// whole session makes no egress attempt and no listen.
	{"github.com/modelcontextprotocol/go-sdk/mcp", "net, net/http, os/exec: client, HTTP and SSE transports and CommandTransport, not used; the server runs on the stdio I/O transport only", nil},
	{"github.com/modelcontextprotocol/go-sdk/auth", "net/http: OAuth for HTTP transports, not used by a stdio server", nil},
	{"github.com/modelcontextprotocol/go-sdk/oauthex", "net, net/http: OAuth discovery and registration clients, not used by a stdio server; its package-level transport is never dialled", nil},
	{"github.com/modelcontextprotocol/go-sdk/internal/util", "net: SplitHostPort for HTTP transports, no connection", nil},
	{"golang.org/x/oauth2", "net/http: OAuth token client, only reached by the SDK's HTTP client transports, not used", nil},
	{"golang.org/x/oauth2/internal", "net/http: as above", nil},
}

var watchedImports = []string{"net", "net/http", "crypto/tls", "os/exec"}

func watched(imp string) bool {
	for _, w := range watchedImports {
		if imp == w {
			return true
		}
	}
	return imp == "golang.org/x/net" || strings.HasPrefix(imp, "golang.org/x/net/")
}

// TestDependencySurface bounds what dependencies could do on their own: a
// dependency that builds its own dialer is invisible at runtime, so the set
// of packages able to do so is reviewed here instead.
func TestDependencySurface(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal("go command not found in CI")
		}
		t.Skip("go command not found; dependency surface not checked")
	}
	root := moduleRoot(t)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			out, err := goList(root, goos)
			if err != nil && strings.Contains(out, "module lookup disabled") {
				// Modules only another OS needs may be missing from the
				// module cache. The test never downloads (no network in
				// tests); CI runs go mod download before testing, so a
				// missing module there is a CI setup defect.
				msg := fmt.Sprintf("module cache lacks %s dependencies; run go mod download first", goos)
				if os.Getenv("GITHUB_ACTIONS") == "true" {
					t.Fatalf("%s\n%s", msg, out)
				}
				t.Skip(msg)
			}
			if err != nil {
				t.Fatalf("go list: %v\n%s", err, out)
			}
			got := map[string]bool{}
			for _, line := range strings.Split(out, "\n") {
				pkg, imports, ok := strings.Cut(strings.TrimSpace(line), ": ")
				if !ok {
					continue
				}
				for _, imp := range strings.Fields(imports) {
					if watched(imp) {
						got[pkg] = true
					}
				}
			}
			want := map[string]bool{}
			for _, d := range networkDeps {
				if d.goos == nil || contains(d.goos, goos) {
					want[d.pkg] = true
				}
			}
			for pkg := range got {
				if !want[pkg] {
					t.Errorf("unreviewed package with network or subprocess imports: %s", pkg)
				}
			}
			for pkg := range want {
				if !got[pkg] {
					t.Errorf("allow-listed package no longer imports network or subprocess packages; remove it: %s", pkg)
				}
			}
			if t.Failed() {
				keys := make([]string, 0, len(got))
				for k := range got {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				t.Logf("found: %s", strings.Join(keys, ", "))
			}
		})
	}
}

// goList lists the dependency imports for goos with module downloads
// disabled, so the test itself never touches the network.
func goList(root, goos string) (string, error) {
	cmd := exec.Command("go", "list", "-deps", "-f", `{{if not .Standard}}{{.ImportPath}}: {{join .Imports " "}}{{end}}`, "./cmd/graphvein")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOFLAGS=-mod=readonly", "CGO_ENABLED=0", "GOPROXY=off")
	// Diagnostics go to stderr and are kept out of the parsed listing.
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return out.String() + stderr.String(), err
	}
	return out.String(), nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
