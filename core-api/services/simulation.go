package services

import (
	"fmt"
	"math/rand"
	"slices"

	"core-api/internal/api/requests"
	"core-api/internal/core"
	"core-api/internal/environments/apache"
	"core-api/internal/environments/nginx"
	"core-api/internal/models"
)

const (
	environmentNginx  = "nginx"
	environmentApache = "apache"

	// decoyLinesPerSource is how many log lines each decoy source adds.
	decoyLinesPerSource = 8

	// maxAttackers caps how many distinct attackers a simulation can have.
	maxAttackers = 3
)

// environmentGen is the set of generators an environment provides. Adding a
// new environment means adding one entry to environments below.
type environmentGen struct {
	selectMissions  func(*rand.Rand, core.DifficultyLevel, int) ([]models.Mission, error)
	newSourceIP     func(*rand.Rand) string
	generateAnomaly func(*rand.Rand, string, string) (models.Anomaly, error)
	generateLog     func(rng *rand.Rand, anomalies []models.Anomaly, attackerOf []int,
		normalCount, decoyLines int, noiseLevel float64) []models.Log
}

var environments = map[string]environmentGen{
	environmentNginx: {
		selectMissions:  nginx.SelectMissions,
		newSourceIP:     nginx.NewSourceIP,
		generateAnomaly: nginx.GenerateAnomaly,
		generateLog:     nginx.GenerateAccessLog,
	},
	environmentApache: {
		selectMissions:  apache.SelectMissions,
		newSourceIP:     apache.NewSourceIP,
		generateAnomaly: apache.GenerateAnomaly,
		generateLog:     apache.GenerateAccessLog,
	},
}

// IsSupportedEnvironment reports whether an environment name is known.
func IsSupportedEnvironment(environment string) bool {
	_, ok := environments[environment]
	return ok
}

// CreateSingleplayer resolves the seed and builds the simulation.
func CreateSingleplayer(req requests.CreateSingleplayerRequest) (models.Simulation, error) {
	if err := req.Validate(); err != nil {
		return models.Simulation{}, &BadRequestError{err.Error()}
	}

	seed, err := core.ResolveSeed(req.Seed)
	if err != nil {
		return models.Simulation{}, err
	}

	return BuildSimulation(seed, req.Environment, req.Difficulty)
}

// BuildSimulation is the deterministic generation pipeline. The same seed,
// environment and difficulty always produce the same simulation, which is how
// submissions are validated without storing anything.
//
// The order of RNG draws below is part of that contract: changing it changes
// what every existing seed generates.
func BuildSimulation(seed, environment, difficulty string) (models.Simulation, error) {
	env, ok := environments[environment]
	if !ok {
		return models.Simulation{}, &BadRequestError{"unsupported environment"}
	}

	cfg, err := core.GetDifficultyConfig(core.DifficultyLevel(difficulty))
	if err != nil {
		return models.Simulation{}, &BadRequestError{err.Error()}
	}

	rng, err := core.NewRNG(seed)
	if err != nil {
		return models.Simulation{}, &BadRequestError{"invalid seed: must be 16 hex characters"}
	}

	selectedMissions, err := env.selectMissions(rng, cfg.Level, cfg.MainAnomalyCount)
	if err != nil {
		return models.Simulation{}, err
	}

	// The number of distinct attackers is seeded too: between 1 and
	// maxAttackers, never more than there are anomalies. One attacker can be
	// behind several anomalies (a multi-stage attack).
	anomalyCount := len(selectedMissions)
	attackerCount := 1
	if limit := min(maxAttackers, anomalyCount); limit > 1 {
		attackerCount = 1 + rng.Intn(limit)
	}

	// Distinct attacker IPs.
	attackerIPs := make([]string, 0, attackerCount)
	for len(attackerIPs) < attackerCount {
		ip := env.newSourceIP(rng)
		if !slices.Contains(attackerIPs, ip) {
			attackerIPs = append(attackerIPs, ip)
		}
	}

	// Every attacker gets at least one anomaly; the rest are spread randomly.
	attackerOf := make([]int, anomalyCount)
	for i := range attackerOf {
		if i < attackerCount {
			attackerOf[i] = i
		} else {
			attackerOf[i] = rng.Intn(attackerCount)
		}
	}

	simulation := models.Simulation{
		Type:          "singleplayer",
		Seed:          seed,
		Environment:   environment,
		Difficulty:    difficulty,
		AttackerCount: attackerCount,
		Missions:      selectedMissions,
	}

	seen := map[string]bool{}
	for i, mission := range selectedMissions {
		anomaly, err := env.generateAnomaly(rng, mission.AnomalyType, attackerIPs[attackerOf[i]])
		if err != nil {
			return models.Simulation{}, err
		}

		// Every anomaly must be uniquely identifiable by its three answers.
		key := anomaly.SourceIP + "|" + anomaly.Endpoint + "|" + anomaly.Type
		if seen[key] {
			return models.Simulation{}, fmt.Errorf("generated duplicate anomaly %s", key)
		}
		seen[key] = true

		simulation.Anomalies = append(simulation.Anomalies, anomaly)
	}

	normalCount := cfg.MinLogLines
	if cfg.MaxLogLines > cfg.MinLogLines {
		normalCount += rng.Intn(cfg.MaxLogLines - cfg.MinLogLines + 1)
	}

	simulation.Logs = env.generateLog(
		rng,
		simulation.Anomalies,
		attackerOf,
		normalCount,
		cfg.DecoyAnomalyCount*decoyLinesPerSource,
		cfg.NoiseLevel,
	)

	return simulation, nil
}
