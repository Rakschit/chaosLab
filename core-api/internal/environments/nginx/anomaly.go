package nginx

import (
	"fmt"
	"math/rand"

	"core-api/internal/models"
)

const (
	AnomalyCredentialBruteForce = "credential_brute_force"
	AnomalyEndpointScanning     = "endpoint_scanning"
	AnomalyPathTraversal        = "path_traversal"
)

// NewSourceIP returns a random private IP, used as an attacker address.
func NewSourceIP(rng *rand.Rand) string {
	return generatePrivateIP(rng)
}

// GenerateAnomaly builds an anomaly of the given type. If sourceIP is empty a
// fresh attacker IP is drawn; pass the same IP for several anomalies to model
// a single attacker running multiple attacks.
func GenerateAnomaly(rng *rand.Rand, anomalyType, sourceIP string) (models.Anomaly, error) {
	if sourceIP == "" {
		sourceIP = generatePrivateIP(rng)
	}

	switch anomalyType {
	case AnomalyCredentialBruteForce:
		endpoints := []string{"/login", "/signin", "/auth/login", "/admin/login"}
		return models.Anomaly{
			Type:     anomalyType,
			SourceIP: sourceIP,
			Endpoint: endpoints[rng.Intn(len(endpoints))],
			Method:   "POST",
		}, nil

	case AnomalyEndpointScanning:
		endpoints := []string{"/admin", "/phpmyadmin", "/.env", "/backup"}
		return models.Anomaly{
			Type:     anomalyType,
			SourceIP: sourceIP,
			Endpoint: endpoints[rng.Intn(len(endpoints))],
			Method:   "GET",
		}, nil

	case AnomalyPathTraversal:
		endpoints := []string{
			"/static/../../etc/passwd",
			"/download?file=../../etc/shadow",
			"/files/..%2f..%2fetc/passwd",
		}
		return models.Anomaly{
			Type:     anomalyType,
			SourceIP: sourceIP,
			Endpoint: endpoints[rng.Intn(len(endpoints))],
			Method:   "GET",
		}, nil
	}

	return models.Anomaly{}, fmt.Errorf("unknown anomaly type %q", anomalyType)
}
