package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RegradeChange is one row whose grade changed in a re-grade.
type RegradeChange struct {
	RunID      string
	Question   string
	OldCorrect bool
	NewCorrect bool
	OldScore   float64
	NewScore   float64
}

// regradeRuns grades every answered row of runs.jsonl again from its
// committed answer file against qs. A row whose grade changes gets the new
// correct, score and grade; its earlier grade moves to grade_v1 (kept from
// the first re-grade only, so grade_v1 is always the grade of the run
// itself) and regraded records note. Other lines stay byte for byte, so the
// diff shows exactly the re-graded rows. With write false nothing is
// written and the changes are only returned.
//
// Grading is deterministic and needs no transcript: the answer file holds
// the final JSON block the run was graded on. Rows that did not answer
// (no_answer, timeout, cost_cap, ...) have nothing to re-grade.
func regradeRuns(dir string, qs []Question, note string, write bool) ([]RegradeChange, error) {
	runsPath := filepath.Join(dir, "runs.jsonl")
	data, err := os.ReadFile(runsPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", runsPath, err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	var changes []RegradeChange
	for i, line := range lines {
		text := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(text) == "" {
			continue
		}
		var r Row
		if err := json.Unmarshal([]byte(text), &r); err != nil {
			return nil, fmt.Errorf("parsing %s line %d: %w", runsPath, i+1, err)
		}
		if r.Grade == nil || !r.Grade.Answered || r.AnswerFile == "" {
			continue
		}
		q, err := findQuestion(qs, r.Question)
		if err != nil {
			return nil, err
		}
		answer, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(r.AnswerFile)))
		if err != nil {
			return nil, fmt.Errorf("%s: reading answer: %w", r.RunID, err)
		}
		g := gradeAnswer(q, "```json\n"+strings.TrimSpace(string(answer))+"\n```\n", "")
		if !g.Answered {
			return nil, fmt.Errorf("%s: the answer file no longer parses as an answer (%s)", r.RunID, g.Problem)
		}
		if marshalPlain(g) == marshalPlain(*r.Grade) && g.Correct == r.Correct && g.Score == r.Score {
			continue
		}
		changes = append(changes, RegradeChange{RunID: r.RunID, Question: r.Question, OldCorrect: r.Correct, NewCorrect: g.Correct, OldScore: r.Score, NewScore: g.Score})
		if r.GradeV1 == nil {
			r.GradeV1 = &GradeRecord{Correct: r.Correct, Score: r.Score, Grade: r.Grade}
		}
		r.Grade = &g
		r.Correct, r.Score = g.Correct, g.Score
		r.Regraded = note
		lines[i] = marshalPlain(r) + line[len(text):]
	}
	if !write || len(changes) == 0 {
		return changes, nil
	}
	out := []byte(strings.Join(lines, ""))
	if err := checkNoSecret(out); err != nil {
		return nil, err
	}
	tmp := runsPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, runsPath); err != nil {
		return nil, fmt.Errorf("replacing %s: %w", runsPath, err)
	}
	return changes, nil
}

// writeRegradeSection lists, per question, how many runs changed and the
// correct counts before and after, when any row carries a grade_v1.
func writeRegradeSection(b *strings.Builder, rows []Row) {
	type count struct{ runs, before, after int }
	by := map[string]*count{}
	notes := map[string]bool{}
	for _, r := range rows {
		if r.GradeV1 == nil {
			continue
		}
		c := by[r.Question]
		if c == nil {
			c = &count{}
			by[r.Question] = c
		}
		c.runs++
		if r.GradeV1.Correct {
			c.before++
		}
		if r.Correct {
			c.after++
		}
		notes[r.Regraded] = true
	}
	if len(by) == 0 {
		return
	}
	b.WriteString("\n## Re-graded runs\n\n")
	b.WriteString("These runs were graded again by `gvbench regrade` after a correction of the question file; ")
	b.WriteString("the tables above use the new grades, and each row keeps its earlier grade in `grade_v1`.\n\n")
	for _, n := range sortedKeys(notes) {
		fmt.Fprintf(b, "- %s\n", n)
	}
	b.WriteString("\n| Question | Runs re-graded | Correct before | Correct after |\n|---|---|---|---|\n")
	qs := sortedKeys(by)
	sort.SliceStable(qs, func(i, j int) bool { return questionNumber(qs[i]) < questionNumber(qs[j]) })
	for _, q := range qs {
		c := by[q]
		fmt.Fprintf(b, "| %s | %d | %d | %d |\n", q, c.runs, c.before, c.after)
	}
}
