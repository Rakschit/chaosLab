package requests

import "errors"

// Inital Request sent by the user when user start the simulation by selecting singleplayer.
type CreateSingleplayerRequest struct {
	Seed        string `json:"seed"`
	Environment string `json:"environment"`
	Difficulty  string `json:"difficulty"`
}

// If for some reason env and difficulty is not sent through
func (r CreateSingleplayerRequest) Validate() error {
	if r.Environment == "" {
		return errors.New("environment is required")
	}

	if r.Difficulty == "" {
		return errors.New("difficulty is required")
	}

	return nil
}
