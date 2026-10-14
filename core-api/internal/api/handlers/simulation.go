package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"core-api/internal/api/requests"
	"core-api/internal/api/responses"
	"core-api/services"
)

const maxBodyBytes = 1 << 20 // 1 MiB

func CreateSingleplayer(w http.ResponseWriter, r *http.Request) {
	var req requests.CreateSingleplayerRequest

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	simulation, err := services.CreateSingleplayer(req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Monthly limit per plan and difficulty. Counted only for simulations that
	// were created successfully, so invalid requests never use up a game.
	if !allowStart(w, r, simulation.Difficulty) {
		return
	}

	// Remember when this simulation started, to measure the time taken on submit.
	recordStart(r, simulation.Seed, simulation.Environment, simulation.Difficulty, "")

	writeJSON(w, http.StatusOK, responses.FromSimulation(simulation))
}

// writeServiceError maps client errors to 400 and hides internal ones behind a 500.
func writeServiceError(w http.ResponseWriter, err error) {
	var bad *services.BadRequestError
	if errors.As(err, &bad) {
		writeError(w, http.StatusBadRequest, bad.Error())
		return
	}
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, responses.ErrorResponse{Error: message})
}
