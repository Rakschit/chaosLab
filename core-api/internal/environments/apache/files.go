package apache

import (
	"fmt"
	"strings"
	"time"

	"core-api/internal/models"
)

var taskLabels = map[string]string{
	"source_ip":    "Identify the source IP address of the suspicious traffic",
	"endpoint":     "Identify the endpoint being targeted",
	"anomaly_type": "Classify the type of anomaly",
}

// FormatMissionFile renders one mission block for /mission.txt.
func FormatMissionFile(mission models.Mission) string {
	var b strings.Builder

	b.WriteString("MISSION\n\n")
	fmt.Fprintf(&b, "Objective: %s\n\n", mission.Objective)
	b.WriteString("Tasks:\n")

	for _, task := range mission.Tasks {
		label, ok := taskLabels[task]
		if !ok {
			label = task
		}
		fmt.Fprintf(&b, "- %s\n", label)
	}

	return b.String()
}

// Apache 2.4 error log timestamp, e.g. [Tue Sep 29 10:30:00.000000 2026].
const errorLogTimeLayout = "Mon Jan 02 15:04:05.000000 2006"

func errorTime(offset time.Duration) string {
	return captureBase().Add(30*time.Minute + offset).Format(errorLogTimeLayout)
}

// FormatErrorLog renders Apache error.log lines for the anomalies.
func FormatErrorLog(anomalies []models.Anomaly) string {
	var b strings.Builder
	for i, a := range anomalies {
		fmt.Fprintf(&b, "[%s] [authz_core:error] [pid %d] [client %s:%d] AH01630: client denied by server configuration: %s\n",
			errorTime(time.Duration(i)*7*time.Second), 1042+i, a.SourceIP, 40000+i*137, a.Endpoint)
	}
	return b.String()
}

// FormatErrorLogArchive renders the rotated error.log.1 (older entries).
func FormatErrorLogArchive(anomalies []models.Anomaly) string {
	var b strings.Builder
	for i, a := range anomalies {
		fmt.Fprintf(&b, "[%s] [core:error] [pid %d] [client %s:%d] AH10244: invalid URI path: %s\n",
			errorTime(-24*time.Hour+time.Duration(i)*11*time.Second), 977+i, a.SourceIP, 51000+i*211, a.Endpoint)
	}
	return b.String()
}

// FormatAuthLog renders /var/log/auth.log lines for the anomalies.
func FormatAuthLog(anomalies []models.Anomaly) string {
	var b strings.Builder
	for _, a := range anomalies {
		fmt.Fprintf(&b, "%s web01 sshd[1042]: Failed password for invalid user admin from %s port 52814 ssh2\n",
			captureBase().Add(30*time.Minute).Format("Jan  2 15:04:05"), a.SourceIP)
	}
	return b.String()
}

// FormatApacheConfig renders a static apache2.conf.
func FormatApacheConfig() string {
	return `ServerRoot "/etc/apache2"
Listen 80
ServerName example.com

ErrorLog ${APACHE_LOG_DIR}/error.log
LogLevel warn
LogFormat "%h %l %u %t \"%r\" %>s %O \"%{Referer}i\" \"%{User-Agent}i\"" combined
CustomLog ${APACHE_LOG_DIR}/access.log combined

<VirtualHost *:80>
    DocumentRoot /var/www/html

    <Directory /var/www/html>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    <Location "/server-status">
        SetHandler server-status
        Require ip 10.0.0.0/8
    </Location>
</VirtualHost>
`
}

// Files returns the virtual filesystem for a simulation at its difficulty.
func Files(sim models.Simulation) map[string]string {
	var missionText strings.Builder
	for i, m := range sim.Missions {
		if i > 0 {
			missionText.WriteString("\n\n")
		}
		missionText.WriteString(FormatMissionFile(m))
	}

	files := map[string]string{
		"/mission.txt":                missionText.String(),
		"/var/log/apache2/access.log": FormatAccessLog(sim.Logs),
	}

	switch sim.Difficulty {
	case "medium":
		files["/var/log/apache2/error.log"] = FormatErrorLog(sim.Anomalies)
		files["/etc/apache2/apache2.conf"] = FormatApacheConfig()

	case "hard":
		files["/var/log/apache2/error.log"] = FormatErrorLog(sim.Anomalies)
		files["/etc/apache2/apache2.conf"] = FormatApacheConfig()
		files["/var/log/apache2/error.log.1"] = FormatErrorLogArchive(sim.Anomalies)
		files["/var/log/auth.log"] = FormatAuthLog(sim.Anomalies)
	}

	return files
}
