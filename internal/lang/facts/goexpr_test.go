package facts

import (
	"testing"
	"time"
)

// goChain nests depth nodes of one required-child kind above an identifier
// named name: the deepest tree a cached Go payload may hold when depth is
// GoMaxDepth.
func goChain(kind string, depth int, name string) GoExpr {
	e := GoExpr{Kind: GoIdentifier, Name: name}
	for i := 0; i < depth; i++ {
		child := e
		leaf := &GoExpr{Kind: GoIdentifier, Name: "y"}
		switch kind {
		case GoSelector:
			e = GoExpr{Kind: kind, Name: "f", Object: &child}
		case GoCall:
			e = GoExpr{Kind: kind, Callee: &child}
		case GoUnary:
			e = GoExpr{Kind: kind, Value: "-", Object: &child}
		case GoBinary:
			// Alternate the deep side so both required children are exercised.
			if i%2 == 0 {
				e = GoExpr{Kind: kind, Value: "+", Left: &child, Right: leaf}
			} else {
				e = GoExpr{Kind: kind, Value: "+", Left: leaf, Right: &child}
			}
		case GoMap:
			e = GoExpr{Kind: kind, Left: leaf, Right: &child}
		default:
			e = GoExpr{Kind: kind, Object: &child}
		}
	}
	return e
}

// TestGoExprValidIsLinear guards T076: validating a required child twice
// doubled the cost per level, so a cached tree at the depth limit (2^48
// visits) never finished validating on a warm run. Each case must answer at
// once.
func TestGoExprValidIsLinear(t *testing.T) {
	kinds := []string{GoSelector, GoCall, GoUnary, GoBinary, GoSpread, GoIndex, GoPointer, GoSlice, GoArray, GoMap}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, kind := range kinds {
			if e := goChain(kind, GoMaxDepth, "x"); !e.valid(0) {
				t.Errorf("%s chain of depth %d rejected", kind, GoMaxDepth)
			}
			if e := goChain(kind, GoMaxDepth+1, "x"); e.valid(0) {
				t.Errorf("%s chain of depth %d accepted", kind, GoMaxDepth+1)
			}
			// A damaged identifier at the bottom is still found.
			if e := goChain(kind, GoMaxDepth, ""); e.valid(0) {
				t.Errorf("%s chain with an empty identifier at the bottom accepted", kind)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("validation of depth-limit chains did not finish in 30 s (exponential again?)")
	}
}
