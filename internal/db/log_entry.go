package db

import (
	"time"

	"github.com/google/uuid"
)

// Level represents log severity
type Level string

const (
	INFO  Level = "INFO"
	WARN  Level = "WARN"
	ERROR Level = "ERROR"
	DEBUG Level = "DEBUG"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	ID        string    `json:"id"`
	Ts        time.Time `json:"ts"`
	Source    string    `json:"source"`
	Level     Level     `json:"level"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id,omitempty"`
	Process   string    `json:"process,omitempty"`
	Component string    `json:"component,omitempty"`
	Thread    string    `json:"thread,omitempty"`
	UserID    string    `json:"user_id,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

// NewLogEntry creates a new log entry with basic information
func NewLogEntry(source string, level Level, msg string) *LogEntry {
	return &LogEntry{
		ID:      uuid.New().String(),
		Ts:      time.Now(),
		Source:  source,
		Level:   level,
		Message: msg,
	}
}
