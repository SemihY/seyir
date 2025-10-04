package db

import (
	"time"

	"github.com/google/uuid"
)

type Level string

const (
	INFO Level = "INFO"
	WARN Level = "WARN"
	ERROR Level = "ERROR"
	DEBUG Level = "DEBUG"
)

type LogEntry struct {
	ID      string    `json:"id"`
	Ts      time.Time `json:"ts"`
	Source  string    `json:"source"`
	Level   Level     `json:"level"`
	Message string    `json:"message"`
}

func NewLogEntry(source string, level Level, msg string) *LogEntry {
	return &LogEntry{
		ID: uuid.New().String(),
		Ts: time.Now(),
		Source: source,
		Level: level,
		Message: msg,
	}
}
