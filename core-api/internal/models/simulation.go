package models

type Simulation struct {
	Type        string
	Seed        string
	Environment string
	Difficulty  string

	// AttackerCount is how many distinct attackers (IPs) are behind the
	// anomalies. Internal only; deliberately not exposed in responses.
	AttackerCount int

	Missions  []Mission
	Anomalies []Anomaly
	Logs      []Log
}
