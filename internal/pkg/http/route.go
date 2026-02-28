package http

import "github.com/go-chi/chi/v5"

// A RouteProvider is a type that can be used to register routes on a chi.Router
// in the fx HTTP server.
type RouteProvider interface {
	Apply(r chi.Router)
}
