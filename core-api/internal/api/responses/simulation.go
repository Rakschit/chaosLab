package responses

import (
	"strings"

	"core-api/internal/environments/nginx"
	"core-api/internal/models"
)

type SimulationResponse struct {
	Type        string            `json:"type"`
	Seed        string            `json:"seed"`
	Environment string            `json:"environment"`
	Difficulty  string            `json:"difficulty"`
	Files       map[string]string `json:"files"`
}

func FromSimulation(sim models.Simulation) SimulationResponse {
	var missionBuilder strings.Builder

	for i, mission := range sim.Missions {
		if i > 0 {
			missionBuilder.WriteString("\n\n")
		}
		missionBuilder.WriteString(nginx.FormatMissionFile(mission))
	}

	files := map[string]string{
		"/mission.txt":              missionBuilder.String(),
		"/var/log/nginx/access.log": nginx.FormatAccessLog(sim.Logs),
	}

	switch sim.Difficulty {
	case "medium":
		files["/var/log/nginx/error.log"] =
			nginx.FormatErrorLog(sim.Anomalies)

		files["/etc/nginx/nginx.conf"] =
			nginx.FormatNginxConfig()

	case "hard":
		files["/var/log/nginx/error.log"] =
			nginx.FormatErrorLog(sim.Anomalies)

		files["/etc/nginx/nginx.conf"] =
			nginx.FormatNginxConfig()

		files["/var/log/nginx/error.log.1"] =
			nginx.FormatErrorLogArchive(sim.Anomalies)

		files["/var/log/auth.log"] =
			nginx.FormatAuthLog(sim.Anomalies)
	}

	return SimulationResponse{
		Type:        sim.Type,
		Seed:        sim.Seed,
		Environment: sim.Environment,
		Difficulty:  sim.Difficulty,
		Files:       files,
	}
}
