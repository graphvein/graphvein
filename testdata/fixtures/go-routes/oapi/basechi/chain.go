package basechi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Impl wraps the generated server as owncast does: a method returns
// Handler(s), and the mount reads it through a call result
// (New().Handler(): GR-O9, reads the generated router from the
// method's return; the generated routes are composed under /api).
type Impl struct{}

func New() *Impl { return &Impl{} }

func (s *Impl) Handler() http.Handler {
	return Handler(s)
}

func (s *Impl) ListPets(w http.ResponseWriter, r *http.Request) {}

func Mount() *chi.Mux {
	root := chi.NewRouter()
	root.Mount("/api/", New().Handler())
	return root
}
