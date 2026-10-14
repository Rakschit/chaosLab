package handler

import (
	"net/http"

	"auth-api/pkg/handlers"
)

// Handler dispatches on the "_route" query parameter, which vercel.json sets
// through its rewrites (e.g. /api/v1/auth/login -> /api/auth/session?_route=login).
func Handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("_route") {
	case "forgot-password":
		handlers.ForgotPassword(w, r)
	case "reset-password":
		handlers.ResetPassword(w, r)
	case "change-password":
		handlers.ChangePassword(w, r)
	default:
		http.NotFound(w, r)
	}
}
