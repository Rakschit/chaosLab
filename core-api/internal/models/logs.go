package models

import "time"

type Log struct {
	Time     time.Time
	SourceIP string
	Method   string
	Endpoint string
	Status   int
	Bytes    int

	UserAgent string
}
