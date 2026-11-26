package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

const publicQuestions = "../../../docs/evidence/benchmark/questions.jsonl"

func answerText(t *testing.T, obj any) string {
	t.Helper()
	return "Some reasoning.\n\n```json\n" + marshalPlain(obj) + "\n```\n"
}

// Every frozen row must grade its own expected answer as correct: this ties
// the answer schemas, accept ranges and the grader together.
func TestExpectedAnswersGradeCorrect(t *testing.T) {
	qs, err := loadQuestions(publicQuestions)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 21 {
		t.Fatalf("got %d questions, want 21", len(qs))
	}
	for _, q := range qs {
		g := gradeAnswer(q, answerText(t, q.Expected), "")
		if !g.Correct || g.Score != 1 {
			t.Errorf("%s: expected answer graded %+v", q.ID, g)
		}
		if !strings.Contains(buildPrompt(q), "```json") {
			t.Errorf("%s: prompt lacks the answer instruction", q.ID)
		}
	}
}

func TestExtractAnswer(t *testing.T) {
	cases := []struct {
		text, problem string
	}{
		{"no block here", "no_json_block"},
		{"```json\n{\"a\": 1}\n```", ""},
		{"```json\n{\"a\": 1\n```", "invalid_json"},
		{"```json\n[1, 2]\n```", "not_object"},
		{"```json\n{\"a\": 1}\n```\nthen\n```JSON\n{\"b\": 2}\n```", ""},
	}
	for _, c := range cases {
		obj, _, problem := extractAnswer(c.text)
		if problem != c.problem {
			t.Errorf("%q: problem %q, want %q", c.text, problem, c.problem)
		}
		if c.problem == "" && strings.Contains(c.text, "JSON") && obj["b"] == nil {
			t.Errorf("the last block must win: %v", obj)
		}
	}
}

func question(t *testing.T, schema, expected string) Question {
	t.Helper()
	var q Question
	q.ID = "T-1"
	if err := json.Unmarshal([]byte(`{"id":"T-1","corpus":"c","answer_schema":`+schema+`,"expected":`+expected+`}`), &q); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestRouteNormalisation(t *testing.T) {
	q := question(t, `{"routes":["METHOD /path"]}`, `{"routes":["GET /api/v1/items/{id}","POST /api/v1/items/"]}`)
	g := gradeAnswer(q, answerText(t, map[string]any{"routes": []string{"get /api/v1/items/{item_id}", " POST  /api/v1/items/ "}}), "")
	if !g.Correct {
		t.Fatalf("method case and parameter names must not matter: %+v", g)
	}
	g = gradeAnswer(q, answerText(t, map[string]any{"routes": []string{"GET /api/v1/items/{id}", "POST /api/v1/items"}}), "")
	if g.Correct || g.Keys["routes"].F1 != 0.5 {
		t.Fatalf("the trailing slash is significant: %+v", g.Keys["routes"])
	}
	g = gradeAnswer(q, answerText(t, map[string]any{"routes": []string{"GET /api/v1/items/{id}", "GET /api/v1/items/{id}", "POST /api/v1/items/"}}), "")
	if g.Correct || g.Keys["routes"].Matched != 2 || g.Keys["routes"].Predicted != 3 {
		t.Fatalf("a duplicate is a false positive: %+v", g.Keys["routes"])
	}
}

func TestLocationTolerance(t *testing.T) {
	q := question(t, `{"call_sites":["file:line"]}`, `{"call_sites":["src/a.ts:10","src/a.ts:40"]}`)
	cases := []struct {
		have    []string
		matched int
	}{
		{[]string{"src/a.ts:12", "src/a.ts:38"}, 2},
		{[]string{"src/a.ts:13", "src/a.ts:40"}, 1},
		{[]string{"src/a.ts:10", "src/a.ts:11"}, 1}, // one-to-one
		{[]string{`.\src\a.ts:10`, "./src/a.ts:40"}, 2},
		{[]string{"C:/work/ws/src/a.ts:10", `C:\work\ws\src\a.ts:40`}, 2},
		{[]string{"src/a.ts", "src/b.ts:10"}, 0},
	}
	for _, c := range cases {
		g := gradeAnswer(q, answerText(t, map[string]any{"call_sites": c.have}), "C:/work/ws")
		if got := g.Keys["call_sites"].Matched; got != c.matched {
			t.Errorf("%v: matched %d, want %d", c.have, got, c.matched)
		}
	}
}

func TestEmptyExpectedAndMissingKeys(t *testing.T) {
	q := question(t, `{"routes":["METHOD /path"]}`, `{"routes":[]}`)
	if g := gradeAnswer(q, answerText(t, map[string]any{"routes": []string{}}), ""); !g.Correct {
		t.Errorf("empty answer to an empty set must be correct: %+v", g)
	}
	if g := gradeAnswer(q, answerText(t, map[string]any{"routes": []string{"GET /x"}}), ""); g.Correct || g.Score != 0 {
		t.Errorf("a non-empty answer to an empty set scores 0: %+v", g)
	}
	if g := gradeAnswer(q, answerText(t, map[string]any{"other": []string{}}), ""); g.Correct || g.Keys["routes"].Problem != "missing_key" {
		t.Errorf("a missing key scores 0: %+v", g)
	}
	if g := gradeAnswer(q, answerText(t, map[string]any{"routes": "GET /x"}), ""); g.Keys["routes"].Problem != "not_a_string_list" {
		t.Errorf("a scalar where a list is expected: %+v", g)
	}
	if g := gradeAnswer(q, "no block", ""); g.Answered || g.Problem != "no_json_block" {
		t.Errorf("no block: %+v", g)
	}
}

func TestSchemaAtoms(t *testing.T) {
	q := question(t,
		`{"columns":["table.column"],"foreign_keys":["table.column -> table.column ON DELETE ACTION"],"conditions":["SETTING == value"],"pairs":["METHOD /path @ file:line"]}`,
		`{"columns":["item.id"],"foreign_keys":["item.owner_id -> user.id ON DELETE CASCADE"],"conditions":["FASTAPI_ENV == development"],"pairs":["GET /api/x/ @ src/a.ts:20"]}`)
	g := gradeAnswer(q, answerText(t, map[string]any{
		"columns":      []string{"Item.ID"},
		"foreign_keys": []string{"item.owner_id->user.id on delete  cascade"},
		"conditions":   []string{`FASTAPI_ENV=="development"`},
		"pairs":        []string{"get /api/x/ @ ./src/a.ts:22"},
	}), "")
	if !g.Correct {
		t.Fatalf("normalised atoms must match: %+v", g.Keys)
	}
	g = gradeAnswer(q, answerText(t, map[string]any{
		"columns":      []string{"item.id"},
		"foreign_keys": []string{"item.owner_id -> user.id ON DELETE SET NULL"},
		"conditions":   []string{"FASTAPI_ENV == development"},
		"pairs":        []string{"POST /api/x/ @ src/a.ts:20"},
	}), "")
	if g.Correct || g.Keys["foreign_keys"].F1 != 0 || g.Keys["pairs"].F1 != 0 || g.Score != 0.5 {
		t.Fatalf("wrong action and wrong method must not match: %+v", g)
	}
}

func TestHandlerRange(t *testing.T) {
	qs, err := loadQuestions(publicQuestions)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := findQuestion(qs, "BQ-13")
	for line, want := range map[int]bool{467: false, 468: true, 474: true, 475: false} {
		ans := map[string]any{"handler": "src/lfx/src/lfx/workflow/router.py:" + strconv.Itoa(line)}
		if g := gradeAnswer(q, answerText(t, ans), ""); g.Correct != want {
			t.Errorf("line %d: correct=%v, want %v", line, g.Correct, want)
		}
	}
}
