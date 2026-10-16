package apache

import (
	"fmt"
	"math/rand"

	"core-api/internal/core"
	"core-api/internal/models"
)

var defaultTasks = []string{"source_ip", "endpoint", "anomaly_type"}

var missions = []models.Mission{
	// Easy
	{ID: "APA_BRUTE_001", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyEasy),
		Objective: "Someone keeps failing to log in to the site. Find where the attempts come from.", Tasks: defaultTasks},
	{ID: "APA_SCAN_001", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyEasy),
		Objective: "A client keeps requesting pages that do not exist. Identify the source.", Tasks: defaultTasks},
	{ID: "APA_PATH_001", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyEasy),
		Objective: "A request is trying to read system files through the web server. Find it.", Tasks: defaultTasks},

	// Medium
	{ID: "APA_BRUTE_002", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyMedium),
		Objective: "Find the client hammering a CMS login or XML-RPC endpoint.", Tasks: defaultTasks},
	{ID: "APA_BRUTE_003", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyMedium),
		Objective: "Repeated 401 and 403 responses point to a password-guessing tool. Locate it.", Tasks: defaultTasks},
	{ID: "APA_SCAN_002", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyMedium),
		Objective: "Someone is probing for Apache status pages, CGI scripts and password files.", Tasks: defaultTasks},
	{ID: "APA_SCAN_003", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyMedium),
		Objective: "Identify the client enumerating sensitive paths on the server.", Tasks: defaultTasks},
	{ID: "APA_PATH_002", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyMedium),
		Objective: "Requests are trying to climb out of the document root. Find the source.", Tasks: defaultTasks},
	{ID: "APA_PATH_003", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyMedium),
		Objective: "Investigate encoded ../ sequences in request paths.", Tasks: defaultTasks},

	// Hard
	{ID: "APA_MULTI_001", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyHard),
		Objective: "Reconstruct the reconnaissance stage of the attack.", Tasks: defaultTasks},
	{ID: "APA_MULTI_002", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyHard),
		Objective: "A slow credential attack is hidden in heavy traffic. Find it.", Tasks: defaultTasks},
	{ID: "APA_MULTI_003", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyHard),
		Objective: "Find the attempt to read files outside the document root.", Tasks: defaultTasks},
	{ID: "APA_MULTI_004", AnomalyType: AnomalyEndpointScanning, Difficulty: string(core.DifficultyHard),
		Objective: "Identify automated probing of sensitive Apache paths among decoy bot traffic.", Tasks: defaultTasks},
	{ID: "APA_MULTI_005", AnomalyType: AnomalyCredentialBruteForce, Difficulty: string(core.DifficultyHard),
		Objective: "Identify repeated failed logins against an authentication endpoint.", Tasks: defaultTasks},
	{ID: "APA_MULTI_006", AnomalyType: AnomalyPathTraversal, Difficulty: string(core.DifficultyHard),
		Objective: "Identify directory traversal payloads in request paths.", Tasks: defaultTasks},
}

// SelectMissions picks `count` missions for a difficulty. It prefers distinct
// anomaly types first, so combined anomalies differ from each other and every
// finding stays uniquely identifiable.
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
