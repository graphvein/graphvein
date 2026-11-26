package index

import (
	"context"
	"errors"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

func TestTypeScriptCapturePanicDiagnostic(t *testing.T) {
	for run := 0; run < 2; run++ {
		_, err := captureTypeScriptUsing(nil, nil, func(*parse.Node, []contract.Node) typescript.TemplateSyntax { panic("injected capture failure") })
		if err == nil {
			t.Fatal("capture panic was not recovered")
		}
		d := syntaxCaptureDiagnostic("host.ts", err)
		if d.File != "host.ts" || d.Severity != "warning" || d.Code != "angular_syntax_capture_failed" {
			t.Fatal(d)
		}
	}
}

func TestInlineFailureIsolation(t *testing.T) {
	for _, failure := range []string{"deadline", "panic"} {
		t.Run(failure, func(t *testing.T) {
			files := []scan.File{{Record: contract.File{Path: "host.ts"}, Source: []byte("badgood")}}
			syntax := map[string]typescript.TemplateSyntax{"host.ts": {Inline: []typescript.InlineTemplate{{ContentStart: 0, ContentEnd: 3, ContentSpan: &contract.Span{StartLine: 1, EndLine: 1}}, {ContentStart: 3, ContentEnd: 7, ContentSpan: &contract.Span{StartLine: 1, EndLine: 1, StartColumn: 3}}}}}
			fresh := map[string]bool{"host.ts": true}
			pipeline := func(ctx context.Context, jobs []parse.Job, opt parse.Options, consume func(int, parse.Result)) error {
				consume(0, parse.Result{Err: context.DeadlineExceeded})
				root := &parse.Node{Kind: "document"}
				if failure == "panic" {
					consume(0, parse.Result{})
				}
				consume(1, parse.Result{Root: root})
				return nil
			}
			if err := captureInline(context.Background(), files, syntax, fresh, parse.Options{}, pipeline); err != nil {
				t.Fatal(err)
			}
			in := syntax["host.ts"].Inline
			captured := syntax["host.ts"]
			if in[0].Reason == "" || in[1].Facts == nil || !in[1].Facts.Valid() || !fresh["host.ts"] || captured.Valid() {
				t.Fatal(in, fresh)
			}
		})
	}
}

func TestInlineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	files := []scan.File{{Record: contract.File{Path: "host.ts"}, Source: []byte("x")}}
	syntax := map[string]typescript.TemplateSyntax{"host.ts": {Inline: []typescript.InlineTemplate{{ContentEnd: 1}}}}
	err := captureInline(ctx, files, syntax, map[string]bool{"host.ts": true}, parse.Options{}, func(ctx context.Context, _ []parse.Job, _ parse.Options, _ func(int, parse.Result)) error {
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
