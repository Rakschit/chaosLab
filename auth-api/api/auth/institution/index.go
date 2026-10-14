package handler

import (
	"net/http"

	"auth-api/pkg/handlers"
)

// Handler dispatches on the "_route" query parameter, which vercel.json sets
// through its rewrites (e.g. /api/v1/auth/institution/start ->
// /api/auth/institution?_route=start).
func Handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("_route") {
	case "start":
		handlers.InstitutionStart(w, r)
	case "callback":
		handlers.InstitutionCallback(w, r)
	case "roster":
		handlers.InstitutionRoster(w, r)
	case "admin-institutions":
		handlers.AdminInstitutions(w, r)
	case "admin-institution-domain":
		handlers.AdminInstitutionDomain(w, r)
	case "admin-institution-license":
		handlers.AdminInstitutionLicense(w, r)
	case "institution-request":
		handlers.InstitutionRequest(w, r)
	case "admin-institution-requests":
		handlers.AdminInstitutionRequests(w, r)
	case "admin-institution-request-decision":
		handlers.AdminInstitutionRequestDecision(w, r)
	case "access-request":
		handlers.InstitutionAccessRequest(w, r)
	case "access-requests":
		handlers.InstitutionAccessRequests(w, r)
	case "access-decision":
		handlers.InstitutionAccessDecision(w, r)
	case "limits":
		handlers.InstitutionLimits(w, r)
	default:
		http.NotFound(w, r)
	}
}