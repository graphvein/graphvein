package routes

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestGoGeneratedServerRefusals checks row GR-G1's refusals on the
// fixture's chi server: options that are no composite literal and a
// BaseRouter that is no name (receiver_unproven: no route), a server no
// visible call instantiates (routes without a path, route_unmounted), and
// an instance in conditional context (conditional path entries).
func TestGoGeneratedServerRefusals(t *testing.T) {
	gen, err := os.ReadFile(goFixture + "/oapi/petchi/petchi.gen.go")
	if err != nil {
		t.Fatal(err)
	}
	mod := "module example.com/goroutes\n\ngo 1.24\n\nrequire github.com/go-chi/chi/v5 v5.2.4\n"
	app := func(body string) map[string]string {
		return map[string]string{
			"go.mod":                    mod,
			"oapi/petchi/petchi.gen.go": string(gen),
			"app/app.go": "package app\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/go-chi/chi/v5\"\n\n\t\"example.com/goroutes/oapi/petchi\"\n)\n\n" +
				"type Pets struct{}\n\nfunc (p *Pets) ListPets(w http.ResponseWriter, r *http.Request) {}\n\n" +
				"func Serve(ok bool, o petchi.ChiServerOptions) error {\n\tsrv := &Pets{}\n\tr := chi.NewRouter()\n" + body + "\treturn http.ListenAndServe(\":80\", r)\n}\n",
		}
	}
	cases := []struct {
		name, body string
		want       []string
	}{
		{"options variable", "\tpetchi.HandlerWithOptions(srv, o)\n", []string{
			"diag oapi/petchi/petchi.gen.go go_routes_unread 2 Go router forms are outside the route claim contract and claim nothing (receiver_unproven=2)",
		}},
		{"BaseRouter not a name", "\tpetchi.HandlerWithOptions(srv, petchi.ChiServerOptions{BaseRouter: chi.NewRouter()})\n", []string{
			"diag oapi/petchi/petchi.gen.go go_routes_unread 2 Go router forms are outside the route claim contract and claim nothing (receiver_unproven=2)",
		}},
		{"no instance", "", []string{
			"diag oapi/petchi/petchi.gen.go route_unmounted GET \"/pets\" (no resolved path from a root) at 126:2-126:50",
			"diag oapi/petchi/petchi.gen.go route_unmounted GET \"/pets/{petId}\" (no resolved path from a root) at 129:2-129:61",
			"route:oapi/petchi/petchi.gen.go#GET%20%2Fpets fw=chi expr=ServerInterface.ListPets",
			"route:oapi/petchi/petchi.gen.go#GET%20%2Fpets%2F%7BpetId%7D fw=chi expr=ServerInterface.ShowPetById",
		}},
		{"conditional instance", "\tif ok {\n\t\tpetchi.HandlerFromMux(srv, r)\n\t}\n", []string{
			"exposes method:app/app.go#Pets.ListPets -> route:oapi/petchi/petchi.gen.go#GET%20%2Fpets",
			"route:oapi/petchi/petchi.gen.go#GET%20%2Fpets fw=chi handler=method:app/app.go#Pets.ListPets paths=/pets cond",
			"route:oapi/petchi/petchi.gen.go#GET%20%2Fpets%2F%7BpetId%7D fw=chi expr=ServerInterface.ShowPetById paths=/pets/{petId} cond",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, edges, diags := goLink(t, app(c.body), nil)
			var got []string
			for _, l := range goRouteLines(nodes, edges, diags) {
				if strings.Contains(l, "app/app.go go_routes_unread") {
					continue
				}
				got = append(got, l)
			}
			for _, n := range nodes {
				for _, p := range n.Attributes["paths"].([]goPathEntry) {
					suffix := " paths=" + p.Template
					if p.Conditional {
						suffix += " cond"
					}
					for j := range got {
						if strings.HasPrefix(got[j], n.Identity+" ") {
							got[j] += suffix
						}
					}
				}
			}
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(c.want, "\n  "))
			}
		})
	}
}
