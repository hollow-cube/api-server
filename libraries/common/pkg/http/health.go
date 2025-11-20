package http

import "net/http"

type AliveHandler struct {
}

func (*AliveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

type ReadyHandler struct {
}

func (*ReadyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
