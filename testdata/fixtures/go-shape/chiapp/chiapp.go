package chiapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Pet is a response body.
type Pet struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Tag   *string `json:"tag,omitempty"`
	Owner *Owner  `json:"owner"`
}

// Owner is nested in Pet.
type Owner struct {
	Name string `json:"name"`
}

// NewPet is a request body decoded with encoding/json.
type NewPet struct {
	Name string `json:"name"`
	Tag  string `json:"tag"`
}

// Types lists the fixture's types for the encoding/json oracle.
var Types = map[string]any{"Pet": Pet{}, "Owner": Owner{}, "NewPet": NewPet{}}

// Server holds the handlers that are methods.
type Server struct{}

// Router registers the chi routes.
func Router(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Get("/pets/{petID}", s.GetPet)
	r.Post("/pets", CreatePet)
	r.Get("/pets", ListPets)
	r.Delete("/pets/{petID}", s.DeletePet)
	r.Put("/pets/{petID}", UpdatePet)
	r.Post("/form", FormHandler)
	r.Get("/text", Text)
	return r
}

// GetPet reads a converted path parameter and a header and answers
// through a helper.
func (s *Server) GetPet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "petID"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if r.Header.Get("If-None-Match") != "" {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.writeJSON(w, http.StatusOK, Pet{ID: id})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// CreatePet decodes a body and checks the error.
func CreatePet(w http.ResponseWriter, r *http.Request) {
	var in NewPet
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(Pet{Name: in.Name})
}

// ListPets reads query parameters; its status is set in a branch.
func ListPets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := q.Get("limit")
	_ = q.Has("cursor")
	_ = r.URL.Query().Get("tag")
	if limit == "0" {
		w.WriteHeader(http.StatusNoContent)
	}
	json.NewEncoder(w).Encode([]Pet{})
}

// DeletePet converts a path parameter read into a local.
func (s *Server) DeletePet(w http.ResponseWriter, r *http.Request) {
	petID := chi.URLParam(r, "petID")
	if _, err := strconv.Atoi(petID); err != nil {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdatePet ignores the decode error.
func UpdatePet(w http.ResponseWriter, r *http.Request) {
	var p NewPet
	json.NewDecoder(r.Body).Decode(&p)
	w.WriteHeader(http.StatusAccepted)
}

// FormHandler reads form values and a cookie.
func FormHandler(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	_ = r.PostFormValue("email")
	_, _, _ = r.FormFile("avatar")
	if _, err := r.Cookie("sid"); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	fmt.Fprintf(w, "hi %s", name)
}

// Text writes a body with w.Write.
func Text(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("text"))
}
