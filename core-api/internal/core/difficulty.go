package core

import "fmt"

// DifficultyLevel is one of the supported difficulty tiers.
type DifficultyLevel string

const (
	DifficultyEasy   DifficultyLevel = "easy"
	DifficultyMedium DifficultyLevel = "medium"
	DifficultyHard   DifficultyLevel = "hard"
)

// DifficultyConfig controls simulation *complexity*: log volume, how many
// anomalies are combined in one log, how much decoy/noise traffic surrounds
// them.
type DifficultyConfig struct {
	Level       DifficultyLevel
	MinLogLines int
	MaxLogLines int
	// MainAnomalyCount is the number of real anomalies (= missions = findings
	// the player must submit). It is the single source of truth for this.
	MainAnomalyCount int
	// DecoyAnomalyCount is the number of decoy sources (suspicious-looking but
	// harmless). Each one produces several log lines.
	DecoyAnomalyCount int
	// NoiseLevel is the fraction of extra ordinary traffic mixed in.
	NoiseLevel float64
}

// GetDifficultyConfig returns the configuration for a difficulty level.
func GetDifficultyConfig(level DifficultyLevel) (DifficultyConfig, error) {
	switch level {
	case DifficultyEasy:
		return DifficultyConfig{
			Level:             DifficultyEasy,
			MinLogLines:       40,
			MaxLogLines:       80,
			MainAnomalyCount:  1,
			DecoyAnomalyCount: 0,
			NoiseLevel:        0.05,
		}, nil

	case DifficultyMedium:
		return DifficultyConfig{
			Level:             DifficultyMedium,
			MinLogLines:       200,
			MaxLogLines:       500,
			MainAnomalyCount:  2,
			DecoyAnomalyCount: 2,
			NoiseLevel:        0.20,
		}, nil

	case DifficultyHard:
		return DifficultyConfig{
			Level:             DifficultyHard,
			MinLogLines:       1000,
			MaxLogLines:       3000,
			MainAnomalyCount:  3,
			DecoyAnomalyCount: 4,
			NoiseLevel:        0.40,
		}, nil

	default:
		return DifficultyConfig{}, fmt.Errorf("unknown difficulty %q", level)
	}
}
