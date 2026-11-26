package golang

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

const shapeSource = `package p

import (
	"encoding/json"
	"net/http"

	g "github.com/gin-gonic/gin"
)

type T struct{ A int }

func (T) MarshalJSON() ([]byte, error) { return nil, nil }

func (t *T) UnmarshalText(b []byte) error { return nil }

func H(c *g.Context) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		return
	}
	err := c.ShouldBindQuery(&req)
	if err != nil {
		c.Status(400)
		return
	}
	_ = c.ShouldBindUri(&req)
	if c.BindJSON(&req) != nil {
		return
	}
	id := c.Param("id")
	go func() { c.Status(200) }()
	_ = id
}

func N(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(r.Context())
	if r.Method == "GET" {
		http.Error(w, "x", 400)
		return
	}
	json.NewEncoder(w).Encode(append([]string{}, r.Header["X"]...))
}

func Plain(x int) {
	f := func(w http.ResponseWriter, r *http.Request) {}
	_ = f
}
`

// TestCaptureShapes checks the shape payload of task T029:
// carrier parameters by role, Checked for the two err-check forms,
// closures, blocks ending in return, reassigned carriers, literal records
// and marshaling methods.
func TestCaptureShapes(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", shapeSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	shapes, marshalers := captureShapes(f, fset)
	if !reflect.DeepEqual(marshalers, []facts.GoMarshaler{{Receiver: "T", Name: "MarshalJSON", Line: 12}, {Receiver: "T", Name: "UnmarshalText", Line: 14}}) {
		t.Errorf("marshalers %+v", marshalers)
	}
	if len(shapes) != 3 || shapes[0].Name != "H" || shapes[1].Name != "N" || shapes[2].Name != "" || shapes[2].Line != 45 || shapes[2].Column != 7 {
		t.Fatalf("shapes %+v", shapes)
	}
	h := shapes[0]
	if len(h.Params) != 1 || h.Params[0].Carrier != facts.GoCarrierGin || h.Params[0].Name != "c" {
		t.Errorf("params %+v", h.Params)
	}
	checked := map[string]bool{}
	closure := map[string]bool{}
	for _, c := range h.Calls {
		checked[c.Call.Callee.Name] = c.Checked
		closure[c.Call.Callee.Name] = c.Closure
	}
	want := map[string]bool{"ShouldBindJSON": true, "ShouldBindQuery": true, "ShouldBindUri": false, "BindJSON": true, "Param": false}
	for m, w := range want {
		if checked[m] != w {
			t.Errorf("%s checked %v, want %v", m, checked[m], w)
		}
	}
	if !closure["Status"] {
		// c.Status(400) and c.Status(200): the last one read is the closure's.
		t.Errorf("closure %v", closure)
	}
	if len(h.Locals) != 3 || h.Locals[0].Name != "req" || h.Locals[0].Type == nil || h.Locals[1].Name != "err" || h.Locals[2].Name != "id" || h.Locals[2].Value == nil {
		t.Errorf("locals %+v", h.Locals)
	}
	n := shapes[1]
	if !reflect.DeepEqual(n.Reassigned, []string{"r"}) || len(n.Terminal) != 1 {
		t.Errorf("reassigned %v terminal %v", n.Reassigned, n.Terminal)
	}
	if n.Params[0].Carrier != facts.GoCarrierWriter || n.Params[1].Carrier != facts.GoCarrierRequest {
		t.Errorf("params %+v", n.Params)
	}
	g := &facts.GoFacts{Version: facts.GoModelsVersion, Shapes: shapes, Marshalers: marshalers}
	if !g.ModelsValid() {
		t.Error("payload is not valid")
	}
}
