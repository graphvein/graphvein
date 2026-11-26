package sequelize

import (
	"encoding/json"
	"os"
	"testing"
)

// TestInflection compares the inflection port with the outputs of
// inflection 1.13.4 (pluralize, singularize) and of sequelize 6.37.8's
// lib/utils.js (underscore, camelize) for every model name, table name,
// attribute key and association option string of both corpora
// (opencollective-api and Gladys at their pins), the investigation probe names
// (Person, Mouse, Archived, BlogPost, ...), the fixture's names, rule
// edge cases, and each of them with an "_id" suffix (the shape of the
// foreign-key names camelize builds). The expected values were produced
// by running those packages in a scratch Node process; null means
// Sequelize throws (camelize of a trailing separator).
func TestInflection(t *testing.T) {
	data, err := os.ReadFile("testdata/inflection-1.13.4.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases map[string]map[string]*string `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) < 1000 {
		t.Fatalf("only %d cases", len(file.Cases))
	}
	funcs := map[string]func(string) (string, bool){
		"pluralize": pluralize, "singularize": singularize, "underscore": underscore, "camelize": camelize,
	}
	for in, want := range file.Cases {
		for name, fn := range funcs {
			got, ok := fn(in)
			w := want[name]
			switch {
			case w == nil && ok:
				t.Errorf("%s(%q) = %q; Sequelize throws", name, in, got)
			case w != nil && !ok:
				t.Errorf("%s(%q) not computed; want %q", name, in, *w)
			case w != nil && got != *w:
				t.Errorf("%s(%q) = %q, want %q", name, in, got, *w)
			}
		}
	}
	// Non-ASCII input is not ported (JavaScript and Go fold case
	// differently).
	for name, fn := range funcs {
		if _, ok := fn("Straße"); ok {
			t.Errorf("%s accepts non-ASCII input", name)
		}
	}
}
