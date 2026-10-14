package nginx

import (
	"core-api/internal/models"
	"fmt"
	"strings"
)

var taskLabels = map[string]string{
	"source_ip":    "Identify the source IP address of the suspicious traffic",
	"endpoint":     "Identify the endpoint being targeted",
	"anomaly_type": "Classify the type of anomaly",
}

func FormatMissionFile(mission models.Mission) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "MISSION\n\n")
	fmt.Fprintf(&builder, "Objective: %s\n\n", mission.Objective)

	builder.WriteString("Tasks:\n")

	for _, task := range mission.Tasks {
		label, ok := taskLabels[task]
		if !ok {
			label = task
		}
		fmt.Fprintf(&builder, "- %s\n", label)
	}

	return builder.String()
}
