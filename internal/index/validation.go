package index

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/scan"
)

func recordLabel(record any) string {
	switch v := record.(type) {
	case contract.Node:
		return fmt.Sprintf("node kind=%s identity=%s", boundedText(v.Kind), boundedText(v.Identity))
	case contract.Edge:
		return fmt.Sprintf("edge kind=%s source=%s target=%s", boundedText(v.Kind), boundedText(v.Source), boundedText(v.Target))
	case contract.Diagnostic:
		return "diagnostic code=" + boundedText(v.Code)
	case contract.File:
		return "file path=" + v.Path
	}
	return fmt.Sprintf("%T", record)
}

// Bound producer-controlled text so rejecting an oversized record cannot itself
// create an oversized diagnostic. Preserve complete UTF-8 code points.
func boundedText(s string) string {
	const limit = 2048
	if len(s) <= limit {
		return s
	}
	end := limit
	for !utf8.ValidString(s[:end]) {
		end--
	}
	return s[:end] + "... (truncated)"
}

func validateRecord(record any) error {
	if err := contract.Validate(record); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(data) >= contract.MaxRecordBytes {
		return fmt.Errorf("record exceeds %d bytes", contract.MaxRecordBytes)
	}
	return nil
}

// checkedDiagnostic returns d unchanged when the contract accepts it. A
// diagnostic the contract rejects (for example one whose file is ".", absolute
// or contains "..") is replaced by an extract_invalid_record diagnostic on its
// own file when that path is valid, or on the run (empty file) otherwise, so
// one extractor's bad diagnostic never stops the whole index run.
func checkedDiagnostic(d contract.Diagnostic) contract.Diagnostic {
	err := validateRecord(d)
	if err == nil {
		return d
	}
	file := ""
	if contract.ValidPath(d.File, false) {
		file = d.File
	}
	slog.Warn("replacing invalid diagnostic", "code", boundedText(d.Code), "file", boundedText(d.File), "error", err)
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "error", Code: "extract_invalid_record",
		Message: fmt.Sprintf("%s file=%q: %s", recordLabel(d), boundedText(d.File), boundedText(err.Error()))}
}

func localRecords(file scan.File, extracted facts.Facts) []any {
	records := []any{typescript.FileNode(file.Record, file.Source)}
	for _, n := range extracted.Nodes {
		records = append(records, n)
	}
	for _, e := range extracted.Edges {
		records = append(records, e)
	}
	for _, d := range extracted.Diagnostics {
		records = append(records, d)
	}
	return records
}

func validateLocal(file scan.File, extracted facts.Facts) (any, error) {
	for _, record := range localRecords(file, extracted) {
		if err := validateRecord(record); err != nil {
			return record, err
		}
	}
	identities := map[string]bool{facts.ID("file", file.Record.Path, file.Record.Path): true}
	for _, n := range extracted.Nodes {
		if n.File != file.Record.Path {
			return n, fmt.Errorf("source node file %q differs from owning file", n.File)
		}
		identities[n.Identity] = true
	}
	for _, e := range extracted.Edges {
		if !identities[e.Source] || !identities[e.Target] {
			return e, fmt.Errorf("local edge endpoints must reference this file's emitted nodes")
		}
		for _, evidence := range e.Evidence {
			if evidence.File != file.Record.Path {
				return e, fmt.Errorf("local edge evidence must reference owning file")
			}
		}
	}
	return nil, nil
}
