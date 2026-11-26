package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/mcp"
)

// mcpCommand is graphvein mcp (ADR 0082): the read-only MCP server over
// stdio. It reads JSON-RPC from stdin and writes it to stdout until the
// client closes stdin; nothing else may write to stdout, so diagnostics go
// to stderr. It opens no socket: there is no listener and no client.
func mcpCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", "", "logical repository ID (defaults to directory name)")
	projectArg := flags.String("project", "", "project manifest (graphvein-project.yaml) or its directory: serve every member and the project's links")
	structured := flags.Bool("structured-only", false, "return structuredContent with a one-line text summary instead of a second JSON copy")
	root, pathFirst := ".", false
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		root, pathFirst = args[0], true
		args = args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && pathFirst) || (*projectArg != "" && (flags.NArg() > 0 || pathFirst)) {
		fmt.Fprintln(stderr, "usage: graphvein mcp [path] [--repository id] [--structured-only], or graphvein mcp --project <manifest or its directory>")
		return 2
	}
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	// Warnings and errors only: the SDK logs every session step at info.
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	opt := mcp.Options{Version: version, Logger: logger}
	if *structured {
		opt.Encoding = mcp.EncodingStructured
	}
	storeDir := ""
	if *projectArg != "" {
		dir, err := projectDir(*projectArg)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		project, err := config.LoadProject(dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		opt.Project, storeDir = &project, project.Dir
	} else {
		absolute, err := filepath.Abs(root)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		var override *string
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "repository" {
				override = repository
			}
		})
		if opt.Repository, err = config.ResolveRepository(absolute, override); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		opt.Root, storeDir = absolute, absolute
	}
	s, err := openReadStore(ctx, storeDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer s.Close()
	opt.Store = s
	server := mcp.New(opt)
	defer server.Close()
	if err := server.Serve(ctx, stdin, stdout); err != nil && ctx.Err() == nil {
		logger.Error("mcp session ended", "error", err)
		return 1
	}
	return 0
}
