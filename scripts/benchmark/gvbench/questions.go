package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Question is one frozen benchmark row. Only the fields the runner needs are
// decoded strictly; the evidence fields stay in the file for readers.
type Question struct {
	ID           string                     `json:"id"`
	Corpus       string                     `json:"corpus"`
	Commit       string                     `json:"commit"`
	Kind         string                     `json:"kind"`
	Question     string                     `json:"question"`
	AnswerSchema map[string]json.RawMessage `json:"answer_schema"`
	Expected     map[string]json.RawMessage `json:"expected"`
	Accept       map[string]AcceptRange     `json:"accept"`
}

// AcceptRange is the decorator..def line range a handler location may name.
type AcceptRange struct {
	File    string `json:"file"`
	LineMin int    `json:"line_min"`
	LineMax int    `json:"line_max"`
}

// loadQuestions reads a JSON Lines question file. Each row must have an id,
// a corpus and an answer schema whose keys all have an expected value, so a
// broken row fails before any agent is started.
func loadQuestions(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening questions %s: %w", path, err)
	}
	defer f.Close()
	var out []Question
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var q Question
		if err := json.Unmarshal([]byte(text), &q); err != nil {
			return nil, fmt.Errorf("parsing %s:%d: %w", path, line, err)
		}
		if q.ID == "" || q.Corpus == "" || len(q.AnswerSchema) == 0 {
			return nil, fmt.Errorf("parsing %s:%d: row needs id, corpus and answer_schema", path, line)
		}
		for key := range q.AnswerSchema {
			if _, ok := q.Expected[key]; !ok {
				return nil, fmt.Errorf("parsing %s:%d: %s has no expected value for %q", path, line, q.ID, key)
			}
			if _, err := atomKindOf(q.AnswerSchema[key]); err != nil {
				return nil, fmt.Errorf("parsing %s:%d: %s key %q: %w", path, line, q.ID, key, err)
			}
		}
		out = append(out, q)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return out, nil
}

func findQuestion(qs []Question, id string) (Question, error) {
	for _, q := range qs {
		if q.ID == id {
			return q, nil
		}
	}
	return Question{}, fmt.Errorf("question %s not in the question file", id)
}

// Preamble and answer instruction are identical in both modes and for every
// agent; the only difference between modes is the attached MCP server.
const (
	promptPreamble = "You are answering a question about the repository in the current directory. Do not modify files.\nUse any tools available to you."
	promptAnswer   = "End your reply with exactly one fenced ```json block containing an object with these keys and value shapes: %s\nWrite file paths relative to the repository root, and routes as the HTTP method followed by the full path including every router prefix."
)

// buildPrompt returns the exact prompt text sent to every agent for q.
func buildPrompt(q Question) string {
	keys := sortedKeys(q.AnswerSchema)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q: %s", k, compactJSON(q.AnswerSchema[k])))
	}
	schema := "{" + strings.Join(parts, ", ") + "}"
	return promptPreamble + "\n\n" + q.Question + "\n\n" + fmt.Sprintf(promptAnswer, schema) + "\n"
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return marshalPlain(v)
}

// marshalPlain encodes v as compact JSON without HTML escaping, so atoms such
// as "a -> b" stay readable in prompts, rows and answer files.
func marshalPlain(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
