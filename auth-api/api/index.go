package handler

import (
	"net/http"

	"auth-api/pkg/handlers"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	handlers.Health(w, r)
}
