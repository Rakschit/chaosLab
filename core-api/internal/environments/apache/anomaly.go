// Package apache is the Apache HTTP Server (httpd) simulation environment.
// It mirrors the nginx environment: same anomaly types and answer format, but
// Apache-specific endpoints, log formats, config and file layout.
package apache

import (
	"fmt"
	"math/rand"

	"core-api/internal/models"
)

// The anomaly type strings are shared with every environment, because
// submissions are validated against them.
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
		endpoints := []string{"/wp-login.php", "/xmlrpc.php", "/user/login", "/administrator/index.php"}
		return models.Anomaly{
			Type:     anomalyType,
			SourceIP: sourceIP,
			Endpoint: endpoints[rng.Intn(len(endpoints))],
			Method:   "POST",
		}, nil

	case AnomalyEndpointScanning:
		endpoints := []string{"/server-status", "/server-info", "/.htpasswd", "/cgi-bin/test.cgi"}
		return models.Anomaly{
			Type:     anomalyType,
			SourceIP: sourceIP,
			Endpoint: endpoints[rng.Intn(len(endpoints))],
			Method:   "GET",
		}, nil

	case AnomalyPathTraversal:
		endpoints := []string{
			"/icons/../../etc/passwd",
			"/cgi-bin/.%2e/.%2e/.%2e/etc/passwd",
			"/index.php?page=../../../../etc/passwd",
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
