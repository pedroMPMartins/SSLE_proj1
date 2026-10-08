package models

import "time"

type Service struct {
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Timestamp time.Time `json:"timestamp"`
}

// Types are one of the following:
// ingestion
// analytics
// storage
// temperature
