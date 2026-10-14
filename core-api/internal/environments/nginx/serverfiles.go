package nginx

import (
	"fmt"
	"strings"

	"core-api/internal/models"
)

const errorLogTimeLayout = "2006/01/02 15:04:05"

// FormatErrorLog renders nginx error.log lines for the anomalies.
func FormatErrorLog(anomalies []models.Anomaly) string {
	var b strings.Builder
	for _, a := range anomalies {
		t := randomTimeFixed()
		fmt.Fprintf(&b, "%s [error] 1#1: *1 access forbidden by rule, client: %s, server: example.com, request: \"%s %s HTTP/1.1\"\n",
			t.Format(errorLogTimeLayout), a.SourceIP, a.Method, a.Endpoint)
	}
	return b.String()
}

// FormatErrorLogArchive renders the rotated error.log.1 (older entries).
func FormatErrorLogArchive(anomalies []models.Anomaly) string {
	var b strings.Builder
	for _, a := range anomalies {
		t := randomTimeFixed().Add(-24 * 3600 * 1e9)
		fmt.Fprintf(&b, "%s [warn] 1#1: *1 suspicious request, client: %s, request: \"%s %s HTTP/1.1\"\n",
			t.Format(errorLogTimeLayout), a.SourceIP, a.Method, a.Endpoint)
	}
	return b.String()
}

// FormatAuthLog renders /var/log/auth.log lines for the anomalies.
func FormatAuthLog(anomalies []models.Anomaly) string {
	var b strings.Builder
	for _, a := range anomalies {
		t := randomTimeFixed()
		fmt.Fprintf(&b, "%s web01 sshd[1042]: Failed password for invalid user admin from %s port 52814 ssh2\n",
			t.Format("Jan  2 15:04:05"), a.SourceIP)
	}
	return b.String()
}

// FormatNginxConfig renders a static nginx.conf.
func FormatNginxConfig() string {
	return `worker_processes auto;

events { worker_connections 1024; }

http {
    include       mime.types;
    default_type  application/octet-stream;
    access_log    /var/log/nginx/access.log;
    error_log     /var/log/nginx/error.log warn;

    server {
        listen 80;
        server_name example.com;

        location / { root /var/www/html; }
        location /admin { allow 10.0.0.0/8; deny all; }
    }
}
`
}
