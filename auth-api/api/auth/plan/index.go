package handler

import (
	"net/http"

	"auth-api/pkg/handlers"
)

// Handler dispatches on the "_route" query parameter, which vercel.json sets
// through its rewrites (e.g. /api/v1/auth/plan-request -> /api/auth/plan?_route=plan-request).
func Handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("_route") {
	case "plan-request":
		handlers.PlanRequest(w, r)
	case "admin-plan-requests":
		handlers.AdminPlanRequests(w, r)
	case "admin-plan-decision":
		handlers.AdminPlanDecision(w, r)
	default:
		http.NotFound(w, r)
	}
}
