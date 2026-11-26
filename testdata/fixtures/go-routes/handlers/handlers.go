// Package handlers holds handlers registered from other packages.
package handlers

import "net/http"

// Health is a module function handler (GR-H1, pkg.F).
func Health(w http.ResponseWriter, r *http.Request) {}

// Users serves users; its methods are method-value handlers (GR-H2).
type Users struct{}

// List lists users.
func (u *Users) List(w http.ResponseWriter, r *http.Request) {}

// Show shows one user.
func (u *Users) Show(w http.ResponseWriter, r *http.Request) {}

// NewUsers returns a *Users: a call of a module function with one declared
// result type states the receiver type (GR-H2).
func NewUsers() *Users { return &Users{} }

// Wrap is middleware; a call of it is a call result handler (GR-H5).
func Wrap(h http.HandlerFunc) http.HandlerFunc { return h }
