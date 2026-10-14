package services

import (
	"fmt"
	"strings"

	"core-api/internal/api/requests"
	"core-api/internal/api/responses"
	"core-api/internal/models"
)

func sameAnswer(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// fieldMatches reports which of the three answers of a finding match an anomaly.
func fieldMatches(f requests.Finding, a models.Anomaly) (ip, endpoint, typ bool) {
	return sameAnswer(f.SourceIP, a.SourceIP),
		sameAnswer(f.Endpoint, a.Endpoint),
		sameAnswer(f.AnomalyType, a.Type)
}

func matchCount(f requests.Finding, a models.Anomaly) int {
	n := 0
	ip, ep, ty := fieldMatches(f, a)
	for _, ok := range []bool{ip, ep, ty} {
		if ok {
			n++
		}
	}
	return n
}

// bestAssignment pairs each finding with a distinct anomaly (or -1) so the
// total number of correct fields is maximal. Findings may come in any order and
// the same anomaly is never credited twice. Sizes are tiny (a handful), so an
// exhaustive search is fine.
func bestAssignment(findings []requests.Finding, anomalies []models.Anomaly) []int {
	best := make([]int, len(findings))
	cur := make([]int, len(findings))
	for i := range best {
		best[i], cur[i] = -1, -1
	}
	bestScore := -1
	used := make([]bool, len(anomalies))

	var rec func(i, score int)
	rec = func(i, score int) {
		if i == len(findings) {
			if score > bestScore {
				bestScore = score
				copy(best, cur)
			}
			return
		}

		cur[i] = -1
		rec(i+1, score) // leave this finding unmatched

		for j := range anomalies {
			if used[j] {
				continue
			}
			used[j] = true
			cur[i] = j
			rec(i+1, score+matchCount(findings[i], anomalies[j]))
			used[j] = false
		}
		cur[i] = -1
	}
	rec(0, 0)

	return best
}

// ValidateSingleplayerSubmission rebuilds the simulation from the seed using
// the exact same pipeline as creation, then matches the player's findings
// against every anomaly.
func ValidateSingleplayerSubmission(req requests.SubmitSingleplayerRequest) (responses.SubmissionResponse, error) {
	sim, err := BuildSimulation(strings.ToLower(strings.TrimSpace(req.Seed)), req.Environment, req.Difficulty)
	if err != nil {
		return responses.SubmissionResponse{}, err
	}

	total := len(sim.Anomalies)
	if len(req.Findings) > total {
		return responses.SubmissionResponse{}, &BadRequestError{
			fmt.Sprintf("too many findings: this simulation has %d anomalies", total),
		}
	}

	assignment := bestAssignment(req.Findings, sim.Anomalies)

	results := make([]responses.FindingResult, len(req.Findings))
	fieldsCorrect := 0
	matched := 0

	for i, idx := range assignment {
		if idx < 0 {
			continue
		}
		ip, ep, ty := fieldMatches(req.Findings[i], sim.Anomalies[idx])
		r := responses.FindingResult{
			SourceIP:    ip,
			Endpoint:    ep,
			AnomalyType: ty,
			Correct:     ip && ep && ty,
		}
		results[i] = r

		for _, ok := range []bool{ip, ep, ty} {
			if ok {
				fieldsCorrect++
			}
		}
		if r.Correct {
			matched++
		}
	}

	score := 0
	if total > 0 {
		score = fieldsCorrect * 100 / (3 * total)
	}

	return responses.SubmissionResponse{
		Correct:  matched == total && len(req.Findings) == total,
		Score:    score,
		Matched:  matched,
		Total:    total,
		Findings: results,
	}, nil
}
