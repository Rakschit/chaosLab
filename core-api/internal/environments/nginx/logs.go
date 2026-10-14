package nginx

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"core-api/internal/models"
)

// GenerateNormalLogs generates ordinary Nginx traffic.
func GenerateNormalLogs(rng *rand.Rand, count int) []models.Log {
	logs := make([]models.Log, 0, count)

	for i := 0; i < count; i++ {
		logs = append(logs, generateNormalLog(rng))
	}

	return logs
}

func generateNormalLog(rng *rand.Rand) models.Log {
	endpoints := []string{
		"/", "/products", "/products/1", "/about", "/contact",
		"/login", "/register", "/dashboard", "/profile",
		"/static/app.js", "/static/style.css", "/api/products",
	}

	methods := []string{"GET", "GET", "GET", "GET", "POST"}
	statuses := []int{200, 200, 200, 200, 201, 304, 404}

	return models.Log{
		Time:     randomTime(rng),
		SourceIP: generatePrivateIP(rng),
		Method:   methods[rng.Intn(len(methods))],
		Endpoint: endpoints[rng.Intn(len(endpoints))],
		Status:   statuses[rng.Intn(len(statuses))],
		Bytes:    rng.Intn(5000) + 200,
	}
}

func generatePrivateIP(rng *rand.Rand) string {
	return fmt.Sprintf(
		"10.%d.%d.%d",
		rng.Intn(256),
		rng.Intn(256),
		rng.Intn(254)+1,
	)
}

func randomTime(rng *rand.Rand) time.Time {
	return randomTimeBetween(rng, 0, 3600)
}

// randomTimeBetween returns a time between startSec and endSec (seconds after
// the base time of the capture window).
func randomTimeBetween(rng *rand.Rand, startSec, endSec int) time.Time {
	base := time.Date(
		2026, 9, 29, 10, 0, 0, 0,
		time.FixedZone("IST", 5*60*60+30*60),
	)

	span := endSec - startSec
	if span < 1 {
		span = 1
	}

	return base.Add(
		time.Duration(startSec+rng.Intn(span)) * time.Second,
	)
}

var scanProbePaths = []string{
	"/admin", "/phpmyadmin", "/.env", "/backup", "/wp-login.php",
	"/config.php", "/.git/config", "/server-status", "/console", "/old",
}

// GenerateAnomalyLogs generates the evidence for one anomaly inside the time
// window [startSec, endSec). The traffic pattern depends on the anomaly type.
func GenerateAnomalyLogs(
	rng *rand.Rand,
	anomaly models.Anomaly,
	startSec, endSec int,
) []models.Log {
	entry := func(method, endpoint string, status, bytes int) models.Log {
		return models.Log{
			Time:     randomTimeBetween(rng, startSec, endSec),
			SourceIP: anomaly.SourceIP,
			Method:   method,
			Endpoint: endpoint,
			Status:   status,
			Bytes:    bytes,
		}
	}

	var logs []models.Log

	switch anomaly.Type {
	case AnomalyCredentialBruteForce:
		// Repeated failed logins against one endpoint.
		statuses := []int{401, 401, 401, 403}
		n := 12 + rng.Intn(9)
		for i := 0; i < n; i++ {
			logs = append(logs, entry(anomaly.Method, anomaly.Endpoint,
				statuses[rng.Intn(len(statuses))], rng.Intn(500)+200))
		}

	case AnomalyEndpointScanning:
		// Hammers the target path, plus single probes of other sensitive paths.
		statuses := []int{404, 404, 403}
		hits := 8 + rng.Intn(5)
		for i := 0; i < hits; i++ {
			logs = append(logs, entry(anomaly.Method, anomaly.Endpoint,
				statuses[rng.Intn(len(statuses))], rng.Intn(300)+150))
		}
		probed := 0
		for _, idx := range rng.Perm(len(scanProbePaths)) {
			if probed == 8 {
				break
			}
			path := scanProbePaths[idx]
			if path == anomaly.Endpoint {
				continue
			}
			probed++
			logs = append(logs, entry(anomaly.Method, path, 404, rng.Intn(300)+150))
		}

	case AnomalyPathTraversal:
		// Repeated attempts to escape the web root.
		statuses := []int{400, 403, 404}
		n := 8 + rng.Intn(5)
		for i := 0; i < n; i++ {
			logs = append(logs, entry(anomaly.Method, anomaly.Endpoint,
				statuses[rng.Intn(len(statuses))], rng.Intn(400)+150))
		}

	default:
		for i := 0; i < 10; i++ {
			logs = append(logs, entry(anomaly.Method, anomaly.Endpoint, 403, rng.Intn(500)+200))
		}
	}

	return logs
}

// GenerateDecoyLogs generates suspicious-looking but non-target traffic.
func GenerateDecoyLogs(
	rng *rand.Rand,
	count int,
) []models.Log {
	logs := make([]models.Log, 0, count)

	endpoints := []string{
		"/admin", "/robots.txt", "/old-page", "/missing",
	}
	statuses := []int{200, 403, 404}

	for i := 0; i < count; i++ {
		logs = append(logs, models.Log{
			Time:     randomTime(rng),
			SourceIP: generatePrivateIP(rng),
			Method:   "GET",
			Endpoint: endpoints[rng.Intn(len(endpoints))],
			Status:   statuses[rng.Intn(len(statuses))],
			Bytes:    rng.Intn(1000) + 100,
		})
	}

	return logs
}

// GenerateAccessLog combines normal traffic, several anomalies, and decoy
// traffic. attackerOf[i] is the attacker behind anomalies[i]. When one attacker
// runs several anomalies, they happen in consecutive slices of the capture
// window (a multi-stage attack); a lone anomaly spreads over the whole window.
// NoiseLevel is a fraction from 0.0 to 1.0.
func GenerateAccessLog(
	rng *rand.Rand,
	anomalies []models.Anomaly,
	attackerOf []int,
	normalCount int,
	decoyLines int,
	noiseLevel float64,
) []models.Log {
	logs := GenerateNormalLogs(rng, normalCount)

	perAttacker := map[int]int{} // anomalies per attacker
	for i := range anomalies {
		perAttacker[attackerOf[i]]++
	}

	position := map[int]int{} // next slot index per attacker
	for i, anomaly := range anomalies {
		attacker := attackerOf[i]

		lo, hi := 0, 3600
		if group := perAttacker[attacker]; group > 1 {
			slot := 3600 / group
			lo = position[attacker] * slot
			hi = lo + slot
		}
		position[attacker]++

		logs = append(logs, GenerateAnomalyLogs(rng, anomaly, lo, hi)...)
	}

	logs = append(logs, GenerateDecoyLogs(rng, decoyLines)...)

	if noiseLevel < 0 {
		noiseLevel = 0
	}
	if noiseLevel > 1 {
		noiseLevel = 1
	}

	noiseCount := int(float64(normalCount) * noiseLevel)
	logs = append(logs, GenerateNormalLogs(rng, noiseCount)...)

	sort.SliceStable(logs, func(i, j int) bool {
		return logs[i].Time.Before(logs[j].Time)
	})

	return logs
}

// FormatAccessLog renders logs in standard Nginx combined-style format.
func FormatAccessLog(logs []models.Log) string {
	var builder strings.Builder

	for _, entry := range logs {
		fmt.Fprintf(
			&builder,
			"%s - - [%s] \"%s %s HTTP/1.1\" %d %d\n",
			entry.SourceIP,
			entry.Time.Format("02/Jan/2006:15:04:05 -0700"),
			entry.Method,
			entry.Endpoint,
			entry.Status,
			entry.Bytes,
		)
	}

	return builder.String()
}

// randomTimeFixed returns the base timestamp used for deterministic server-file entries.
func randomTimeFixed() time.Time {
	return time.Date(2026, 9, 29, 10, 30, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
}
