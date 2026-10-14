package nginx

import (
	"fmt"
	"math/rand"

	"core-api/internal/core"
	"core-api/internal/models"
)

var defaultTasks = []string{"source_ip", "endpoint", "anomaly_type"}

var missions = []models.Mission{
	// Easy
	{ID: "NGX_BRUTE_001", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyEasy),
		Objective: "Identify the source of repeated login failures.", Tasks: defaultTasks},
	{ID: "NGX_BRUTE_002", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyEasy),
		Objective: "Find the source IP responsible for repeated authentication failures.", Tasks: defaultTasks},

	// Medium
	{ID: "NGX_BRUTE_003", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyMedium),
		Objective: "Someone is hammering a login page. Find out who and where.", Tasks: defaultTasks},
	{ID: "NGX_SCAN_001", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyMedium),
		Objective: "Identify suspicious endpoint scanning activity.", Tasks: defaultTasks},
	{ID: "NGX_SCAN_002", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyMedium),
		Objective: "A client is probing for hidden admin and config pages. Find it.", Tasks: defaultTasks},
	{ID: "NGX_PATH_001", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyMedium),
		Objective: "Investigate suspicious path traversal requests.", Tasks: defaultTasks},
	{ID: "NGX_PATH_002", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyMedium),
		Objective: "Requests are trying to escape the web root. Find the source.", Tasks: defaultTasks},

	// Hard
	{ID: "NGX_MULTI_001", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyHard),
		Objective: "Reconstruct the reconnaissance stage of the attack.", Tasks: defaultTasks},
	{ID: "NGX_MULTI_002", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyHard),
		Objective: "Find the credential attack hidden in the traffic.", Tasks: defaultTasks},
	{ID: "NGX_MULTI_003", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyHard),
		Objective: "Find the attempt to read files outside the web root.", Tasks: defaultTasks},
	{ID: "NGX_MULTI_004", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyHard),
		Objective: "Identify automated probing of sensitive paths.", Tasks: defaultTasks},
	{ID: "NGX_MULTI_005", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyHard),
		Objective: "Identify repeated failed logins against an authentication endpoint.", Tasks: defaultTasks},
	{ID: "NGX_MULTI_006", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyHard),
		Objective: "Identify directory traversal payloads in request paths.", Tasks: defaultTasks},
}

// SelectMissions picks `count` missions for a difficulty. It prefers distinct
// anomaly types first, so combined anomalies are different from each other and
// every finding stays uniquely identifiable.
func SelectMissions(
	rng *rand.Rand,
	level core.DifficultyLevel,
	count int,
) ([]models.Mission, error) {
	if rng == nil {
		return nil, fmt.Errorf("random generator cannot be nil")
	}
	if count < 1 {
		return nil, fmt.Errorf("mission count must be at least 1")
	}

	var candidates []models.Mission
	for _, m := range missions {
		if m.Difficulty == string(level) {
			candidates = append(candidates, m)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no missions found for difficulty %q", level)
	}
	if len(candidates) < count {
		return nil, fmt.Errorf(
			"difficulty %q requires %d missions, but only %d are available",
			level, count, len(candidates),
		)
	}

	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	selected := make([]models.Mission, 0, count)
	taken := make([]bool, len(candidates))
	seenTypes := map[string]bool{}

	// Pass 1: distinct anomaly types.
	for i, m := range candidates {
		if len(selected) == count {
			break
		}
		if !seenTypes[m.AnomalyType] {
			seenTypes[m.AnomalyType] = true
			taken[i] = true
			selected = append(selected, m)
		}
	}
	// Pass 2: fill the remainder.
	for i, m := range candidates {
		if len(selected) == count {
			break
		}
		if !taken[i] {
			taken[i] = true
			selected = append(selected, m)
		}
	}

	return selected, nil
}
