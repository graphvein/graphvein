package template

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func TestExpressionBoundaries(t *testing.T) {
	src := []byte(`{{ {text: '}} | false'} | actual:'a|b' || alternative }} tail`)
	ranges := interpolations(src, 0, len(src))
	if len(ranges) != 1 {
		t.Fatal(ranges)
	}
	names := pipeNames(src, ranges[0][0], ranges[0][1])
	var got []string
	for _, r := range names {
		got = append(got, string(src[r[0]:r[1]]))
	}
	if !reflect.DeepEqual(got, []string{"actual"}) {
		t.Fatal(got)
	}
	params := []byte(`x of call('a;b'); track x.id; let i = $index`)
	segments := splitParameters(params, 0, len(params))
	if len(segments) != 3 {
		t.Fatal(segments)
	}
}

func TestFactsPresence(t *testing.T) {
	if (Facts{}).Valid() {
		t.Fatal("absent arrays accepted")
	}
	f := Facts{ErrorSpans: []contract.Span{}, Elements: []Element{}, Attributes: []Attribute{}, Blocks: []Block{}, Pipes: []Pipe{}}
	if !f.Valid() {
		t.Fatal("empty arrays rejected")
	}
	f.Pipes = nil
	if f.Valid() {
		t.Fatal("missing pipes accepted")
	}
}
