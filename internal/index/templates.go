package index

import (
	"context"
	"fmt"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

func isTypeScript(language string) bool {
	return language == "typescript" || language == "javascript" || language == "tsx"
}

func captureHTML(source []byte, root *parse.Node) (out template.Facts, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("capturing template syntax: %v", p)
		}
	}()
	return template.Extract(source, root), nil
}

func captureTypeScript(root *parse.Node, nodes []contract.Node) (out typescript.TemplateSyntax, err error) {
	return captureTypeScriptUsing(root, nodes, typescript.CaptureTemplates)
}

func captureTypeScriptUsing(root *parse.Node, nodes []contract.Node, capture func(*parse.Node, []contract.Node) typescript.TemplateSyntax) (out typescript.TemplateSyntax, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("capturing decorator syntax: %v", p)
		}
	}()
	return capture(root, nodes), nil
}

func syntaxCaptureDiagnostic(file string, err error) contract.Diagnostic {
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "angular_syntax_capture_failed", Message: fmt.Sprintf("%s class decorator metadata capture: %v", file, err)}
}

func captureDeclarationSyntax(root *parse.Node, facts typescript.Facts, templates typescript.TemplateSyntax) (out typescript.DeclarationSyntax, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("capturing declaration syntax: %v", p)
		}
	}()
	return typescript.CaptureDeclarations(root, facts, templates), nil
}

func captureInjectionSyntax(root *parse.Node, facts typescript.Facts) (out typescript.InjectionSyntax, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("capturing injection syntax: %v", p)
		}
	}()
	return typescript.CaptureInjections(root, facts), nil
}

func captureRouterSyntax(root *parse.Node, facts typescript.Facts, declarations typescript.DeclarationSyntax) (out typescript.RouterSyntax, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("capturing router syntax: %v", p)
		}
	}()
	return typescript.CaptureRouter(root, facts, declarations), nil
}

func captureModelSyntax(file string, root *parse.Node, facts typescript.Facts) (out *typescript.ModelSyntax, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, err = nil, fmt.Errorf("capturing model syntax: %v", p)
		}
	}()
	return typescript.CaptureModels(file, root, facts), nil
}

func modelCaptureDiagnostic(file string, err error) contract.Diagnostic {
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "model_syntax_capture_failed", Message: fmt.Sprintf("%s model syntax capture: %v", file, err)}
}

func captureInline(ctx context.Context, files []scan.File, syntax map[string]typescript.TemplateSyntax, fresh map[string]bool, options parse.Options, pipeline func(context.Context, []parse.Job, parse.Options, func(int, parse.Result)) error) error {
	type target struct {
		file   string
		inline int
	}
	var targets []target
	var jobs []parse.Job
	for _, file := range files {
		if !fresh[file.Record.Path] {
			continue
		}
		for i, in := range syntax[file.Record.Path].Inline {
			if in.Reason != "" {
				continue
			}
			jobs = append(jobs, parse.Job{Language: "angular", Source: file.Source[in.ContentStart:in.ContentEnd]})
			targets = append(targets, target{file.Record.Path, i})
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	return pipeline(ctx, jobs, options, func(i int, result parse.Result) {
		t := targets[i]
		s := syntax[t.file]
		in := &s.Inline[t.inline]
		if result.Err != nil {
			in.Reason = "parse_failed"
			return
		}
		captured, err := captureHTML(jobs[i].Source, result.Root)
		if err != nil {
			in.Reason = "extract_failed"
			return
		}
		captured.Map(*in.ContentSpan)
		in.Facts = &captured
	})
}
