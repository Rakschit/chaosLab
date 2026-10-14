package handler

import (
	"net/http"

	"auth-api/pkg/handlers"
)

// Handler dispatches on the "_route" query parameter, which vercel.json sets
// through its rewrites (e.g. /api/v1/auth/login -> /api/auth/session?_route=login).
func Handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("_route") {
	case "profile":
		handlers.UpdateProfile(w, r)
	case "delete-account":
		handlers.DeleteAccount(w, r)
	default:
		http.NotFound(w, r)
	}
}
