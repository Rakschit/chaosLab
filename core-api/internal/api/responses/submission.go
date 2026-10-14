package responses

// FindingResult is the outcome for one submitted finding (same order as the request).
type FindingResult struct {
	Correct     bool `json:"correct"`
	SourceIP    bool `json:"source_ip"`
	Endpoint    bool `json:"endpoint"`
	AnomalyType bool `json:"anomaly_type"`
}

type SubmissionResponse struct {
	Correct  bool            `json:"correct"`  // every anomaly found, every field right
	Score    int             `json:"score"`    // 0-100
	Matched  int             `json:"matched"`  // findings that were fully correct
	Total    int             `json:"total"`    // number of anomalies in the simulation
	Findings []FindingResult `json:"findings"` // per-finding breakdown
}
