package models

type Mission struct {
	ID          string
	AnomalyType string
	Difficulty  string
	Objective   string
	Tasks       []string
}
