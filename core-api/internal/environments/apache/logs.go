package apache

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"core-api/internal/models"
)

var browserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
	"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36",
}

// Tools an attacker is likely to use, per anomaly type.
var attackerAgents = map[string][]string{
	AnomalyCredentialBruteForce: {"Hydra", "WPScan v3.8.25", "python-requests/2.31.0", "curl/8.4.0"},
	AnomalyEndpointScanning:     {"Nikto/2.5.0", "gobuster/3.6", "sqlmap/1.8", "DirBuster-1.0-RC1"},
	AnomalyPathTraversal:        {"curl/8.4.0", "python-requests/2.31.0", "Wget/1.21.4"},
}

// Benign-but-noisy clients seen in decoy traffic.
var decoyAgents = []string{
	"Googlebot/2.1 (+http://www.google.com/bot.html)",
	"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
	"UptimeRobot/2.0",
	"Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)",
}

var scanProbePaths = []string{
	"/server-status", "/server-info", "/.htpasswd", "/.htaccess", "/cgi-bin/test.cgi",
	"/phpmyadmin", "/.env", "/backup.zip", "/manager/html", "/icons/README",
}

func pick(rng *rand.Rand, list []string) string {
	return list[rng.Intn(len(list))]
}

func generatePrivateIP(rng *rand.Rand) string {
	return fmt.Sprintf("10.%d.%d.%d", rng.Intn(256), rng.Intn(256), rng.Intn(254)+1)
}

// captureBase is the start of the one-hour capture window.
func captureBase() time.Time {
	return time.Date(2026, 9, 29, 10, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
}

func randomTime(rng *rand.Rand) time.Time {
	return randomTimeBetween(rng, 0, 3600)
}

// randomTimeBetween returns a time between startSec and endSec (seconds after
// the base time of the capture window).
func randomTimeBetween(rng *rand.Rand, startSec, endSec int) time.Time {
	span := endSec - startSec
	if span < 1 {
		span = 1
	}
	return captureBase().Add(time.Duration(startSec+rng.Intn(span)) * time.Second)
}

// GenerateNormalLogs generates ordinary Apache traffic.
func GenerateNormalLogs(rng *rand.Rand, count int) []models.Log {
	endpoints := []string{
		"/", "/index.html", "/about.html", "/contact.php", "/blog/", "/blog/post-1",
		"/login", "/account", "/shop/cart", "/images/logo.png",
		"/css/site.css", "/js/main.js", "/favicon.ico", "/api/items",
	}
	methods := []string{"GET", "GET", "GET", "GET", "POST"}
	statuses := []int{200, 200, 200, 200, 301, 304, 404}

	logs := make([]models.Log, 0, count)
	for i := 0; i < count; i++ {
		logs = append(logs, models.Log{
			Time:      randomTime(rng),
			SourceIP:  generatePrivateIP(rng),
			Method:    methods[rng.Intn(len(methods))],
			Endpoint:  endpoints[rng.Intn(len(endpoints))],
			Status:    statuses[rng.Intn(len(statuses))],
			Bytes:     rng.Intn(6000) + 250,
			UserAgent: pick(rng, browserAgents),
		})
	}
	return logs
}

// GenerateAnomalyLogs generates the evidence for one anomaly inside the time
// window [startSec, endSec). The traffic pattern depends on the anomaly type.
func GenerateAnomalyLogs(
	rng *rand.Rand,
	anomaly models.Anomaly,
	startSec, endSec int,
) []models.Log {
	agents := attackerAgents[anomaly.Type]
	if len(agents) == 0 {
		agents = []string{"curl/8.4.0"}
	}
	agent := pick(rng, agents)

	entry := func(method, endpoint string, status, bytes int) models.Log {
		return models.Log{
			Time:      randomTimeBetween(rng, startSec, endSec),
			SourceIP:  anomaly.SourceIP,
			Method:    method,
			Endpoint:  endpoint,
			Status:    status,
			Bytes:     bytes,
			UserAgent: agent,
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
		// Repeated attempts to escape the document root.
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

// GenerateDecoyLogs generates suspicious-looking but harmless traffic.
func GenerateDecoyLogs(rng *rand.Rand, count int) []models.Log {
	endpoints := []string{"/robots.txt", "/.htaccess", "/old-page.html", "/missing", "/sitemap.xml"}
	statuses := []int{200, 403, 404}

	logs := make([]models.Log, 0, count)
	for i := 0; i < count; i++ {
		logs = append(logs, models.Log{
			Time:      randomTime(rng),
			SourceIP:  generatePrivateIP(rng),
			Method:    "GET",
			Endpoint:  endpoints[rng.Intn(len(endpoints))],
			Status:    statuses[rng.Intn(len(statuses))],
			Bytes:     rng.Intn(1000) + 100,
			UserAgent: pick(rng, decoyAgents),
		})
	}
	return logs
}

// GenerateAccessLog combines normal traffic, several anomalies, and decoy
// traffic. attackerOf[i] is the attacker behind anomalies[i]. When one attacker
// runs several anomalies, they happen in consecutive slices of the capture
// window (a multi-stage attack); a lone anomaly spreads over the whole window.
// noiseLevel is a fraction from 0.0 to 1.0.
func GenerateAccessLog(
	rng *rand.Rand,
	anomalies []models.Anomaly,
	attackerOf []int,
	normalCount int,
	decoyLines int,
	noiseLevel float64,
) []models.Log {
	logs := GenerateNormalLogs(rng, normalCount)

	perAttacker := map[int]int{}
	for i := range anomalies {
		perAttacker[attackerOf[i]]++
	}

	position := map[int]int{}
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
	logs = append(logs, GenerateNormalLogs(rng, int(float64(normalCount)*noiseLevel))...)

	sort.SliceStable(logs, func(i, j int) bool {
		return logs[i].Time.Before(logs[j].Time)
	})

	return logs
}

// FormatAccessLog renders logs in Apache "combined" format:
// %h %l %u %t "%r" %>s %b "%{Referer}i" "%{User-Agent}i"
func FormatAccessLog(logs []models.Log) string {
	var b strings.Builder
	for _, e := range logs {
		fmt.Fprintf(&b, "%s - - [%s] \"%s %s HTTP/1.1\" %d %d \"-\" \"%s\"\n",
			e.SourceIP,
			e.Time.Format("02/Jan/2006:15:04:05 -0700"),
			e.Method, e.Endpoint, e.Status, e.Bytes, e.UserAgent)
	}
	return b.String()
}
