package requests

import (
	"errors"
	"fmt"
)

// Finding is one anomaly the player reports.
type Finding struct {
	SourceIP    string `json:"source_ip"`
	Endpoint    string `json:"endpoint"`
	AnomalyType string `json:"anomaly_type"`
}

type SubmitSingleplayerRequest struct {
	Seed        string `json:"seed"`
	Environment string `json:"environment"`
	Difficulty  string `json:"difficulty"`

	// Findings holds one entry per anomaly the player found, in any order.
	Findings []Finding `json:"findings"`
}

// maxFindings is a sanity cap; the service also rejects more findings than
// there are anomalies.
const maxFindings = 10

func (r SubmitSingleplayerRequest) Validate() error {
	switch {
	case r.Seed == "":
		return errors.New("seed is required")
	case r.Environment == "":
		return errors.New("environment is required")
	case r.Difficulty == "":
		return errors.New("difficulty is required")
	case len(r.Findings) == 0:
		return errors.New("at least one finding is required")
	case len(r.Findings) > maxFindings:
		return fmt.Errorf("too many findings (max %d)", maxFindings)
	}

	for i, f := range r.Findings {
		switch {
		case f.SourceIP == "":
			return fmt.Errorf("findings[%d]: source_ip is required", i)
		case f.Endpoint == "":
			return fmt.Errorf("findings[%d]: endpoint is required", i)
		case f.AnomalyType == "":
			return fmt.Errorf("findings[%d]: anomaly_type is required", i)
		}
	}
	return nil
}
