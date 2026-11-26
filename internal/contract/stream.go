package contract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// MaxRecordBytes bounds memory usage for untrusted external indexer output.
const MaxRecordBytes = 8 * 1024 * 1024

// Event associates recoverable validation failures with their original wire line.
type Event struct {
	Line        int
	Record      any
	Diagnostics []Diagnostic
}

// Reader validates framing before allowing a complete snapshot to be published.
type Reader struct {
	scanner *bufio.Scanner
	line    int
	counts  Counts
	header  bool
	done    bool
}

// NewReader constructs a bounded, streaming validator without loading a repository.
func NewReader(r io.Reader) *Reader {
	s := bufio.NewScanner(r)
	s.Split(func(data []byte, eof bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if eof && len(data) > 0 {
			return 0, nil, fmt.Errorf("record requires final LF")
		}
		return 0, nil, nil
	})
	s.Buffer(make([]byte, 4096), MaxRecordBytes+1)
	return &Reader{scanner: s}
}

func count(c *Counts, typ string) {
	c.Records++
	switch typ {
	case "file":
		c.File++
	case "node":
		c.Node++
	case "edge":
		c.Edge++
	case "diagnostic":
		c.Diagnostic++
	}
}

// Next returns invalid body records as diagnostics, and invalid framing as errors.
func (r *Reader) Next() (Event, error) {
	if r.done {
		return Event{}, io.EOF
	}
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return Event{}, fmt.Errorf("line %d: %w", r.line+1, err)
		}
		return Event{}, fmt.Errorf("line %d: truncated stream: missing footer", r.line+1)
	}
	r.line++
	data := r.scanner.Bytes()
	if len(data) >= MaxRecordBytes {
		return Event{}, fmt.Errorf("line %d: record exceeds size limit", r.line)
	}
	event := Event{Line: r.line, Diagnostics: []Diagnostic{}}
	var envelope struct {
		Type string `json:"type"`
		File string `json:"file"`
	}
	_ = json.Unmarshal(data, &envelope)
	value, unknown, err := Decode(data)
	if !utf8.Valid(data) || bytes.ContainsRune(data, '\r') {
		err = fmt.Errorf("record must be UTF-8 with LF line endings")
	}
	if !r.header {
		if err != nil {
			return event, fmt.Errorf("line %d: invalid header: %w", r.line, err)
		}
		if _, ok := value.(*Header); !ok {
			return event, fmt.Errorf("line %d: first record must be header", r.line)
		}
		r.header = true
	} else if envelope.Type == "header" {
		return event, fmt.Errorf("line %d: duplicate header", r.line)
	} else if envelope.Type == "footer" {
		if err != nil {
			return event, fmt.Errorf("line %d: invalid footer: %w", r.line, err)
		}
		if value.(*Footer).Counts != r.counts {
			return event, fmt.Errorf("line %d: footer counts do not match wire records", r.line)
		}
		if r.scanner.Scan() {
			return event, fmt.Errorf("line %d: records after footer", r.line+1)
		}
		if err := r.scanner.Err(); err != nil {
			return event, fmt.Errorf("line %d: %w", r.line+1, err)
		}
		r.done = true
	} else {
		count(&r.counts, envelope.Type)
	}
	if err != nil {
		file := envelope.File
		if !ValidPath(file, false) {
			file = ""
		}
		event.Diagnostics = append(event.Diagnostics, Diagnostic{"diagnostic", file, "error", "invalid_record", fmt.Sprintf("line %d: %v", r.line, err)})
		return event, nil
	}
	event.Record = value
	for _, field := range unknown {
		event.Diagnostics = append(event.Diagnostics, Diagnostic{"diagnostic", "", "warning", "unknown_field", fmt.Sprintf("line %d: ignored field %s", r.line, field)})
	}
	return event, nil
}

// Writer emits canonical framing and counts for future in-process indexers.
type Writer struct {
	out     io.Writer
	counts  Counts
	started bool
	closed  bool
	failed  error
}

// NewWriter keeps framing rules identical for builtin and external fixture producers.
func NewWriter(w io.Writer) *Writer { return &Writer{out: w} }

// Write rejects invalid records instead of emitting facts another consumer cannot use.
func (w *Writer) Write(record any) (result error) {
	line := w.counts.Records + 1
	if w.started {
		line++
	}
	defer func() {
		if result != nil {
			result = fmt.Errorf("line %d: %w", line, result)
		}
	}()
	if w.failed != nil {
		return w.failed
	}
	if w.closed {
		return fmt.Errorf("stream is closed")
	}
	if err := Validate(record); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var envelope struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(data, &envelope)
	if !w.started && envelope.Type != "header" {
		return fmt.Errorf("first record must be header")
	}
	if w.started && envelope.Type == "header" {
		return fmt.Errorf("duplicate header")
	}
	if envelope.Type == "footer" {
		return fmt.Errorf("use Close to write footer")
	}
	if len(data) >= MaxRecordBytes {
		return fmt.Errorf("record exceeds %d bytes", MaxRecordBytes)
	}
	if err := w.emit(data); err != nil {
		return err
	}
	if envelope.Type == "header" {
		w.started = true
	} else {
		count(&w.counts, envelope.Type)
	}
	return nil
}

// Close writes the footer only after all records have been emitted successfully.
func (w *Writer) Close() (result error) {
	defer func() {
		if result != nil {
			result = fmt.Errorf("line %d (footer): %w", w.counts.Records+2, result)
		}
	}()
	if w.failed != nil {
		return w.failed
	}
	if !w.started || w.closed {
		return fmt.Errorf("stream is not open")
	}
	data, err := json.Marshal(Footer{"footer", w.counts})
	if err != nil {
		return err
	}
	if err := w.emit(data); err != nil {
		return err
	}
	w.closed = true
	return nil
}

func (w *Writer) emit(data []byte) error {
	line := append(data, '\n')
	n, err := w.out.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.failed = fmt.Errorf("writing contract stream: %w", err)
		return w.failed
	}
	return nil
}
