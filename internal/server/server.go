package server

import (
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// LogEntry represents a log entry for API responses
type LogEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}
