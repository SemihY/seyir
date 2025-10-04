package collector

import (
	"context"
	"logspot/internal/db"
	"logspot/internal/tail"
	"strings"
)

// LogSource represents a source of logs (stdin, docker, file, etc.)
type LogSource interface {
	// Start begins collecting logs from this source
	Start(ctx context.Context) error
	
	// Stop gracefully stops log collection
	Stop() error
	
	// Name returns a human-readable name for this source
	Name() string
	
	// IsHealthy returns true if the source is functioning properly
	IsHealthy() bool
}

// BaseCollector provides common functionality for all log sources
type BaseCollector struct {
	database   *db.DB
	sourceName string
	isRunning  bool
	stopChan   chan struct{}
}

// NewBaseCollector creates a new base collector
func NewBaseCollector(database *db.DB, sourceName string) *BaseCollector {
	return &BaseCollector{
		database:   database,
		sourceName: sourceName,
		stopChan:   make(chan struct{}),
	}
}

// SaveAndBroadcast saves a log entry and broadcasts it to connected clients
func (bc *BaseCollector) SaveAndBroadcast(entry *db.LogEntry) {
	// Save to database
	db.SaveLog(bc.database, entry)
	
	// Broadcast to live viewers
	tail.BroadcastLog(entry)
}

// ParseLogLevel attempts to detect the log level from the message
func (bc *BaseCollector) ParseLogLevel(message string) db.Level {
	switch {
	case containsIgnoreCase(message, "[ERROR]", "ERROR:", "error:", "FATAL", "fatal"):
		return db.ERROR
	case containsIgnoreCase(message, "[WARN]", "WARN:", "warn:", "WARNING", "warning"):
		return db.WARN
	case containsIgnoreCase(message, "[DEBUG]", "DEBUG:", "debug:", "TRACE", "trace"):
		return db.DEBUG
	default:
		return db.INFO
	}
}

// IsRunning returns true if the collector is currently running
func (bc *BaseCollector) IsRunning() bool {
	return bc.isRunning
}

// SetRunning updates the running status
func (bc *BaseCollector) SetRunning(running bool) {
	bc.isRunning = running
}

// StopChan returns the stop channel for graceful shutdown
func (bc *BaseCollector) StopChan() <-chan struct{} {
	return bc.stopChan
}

// RequestStop signals the collector to stop
func (bc *BaseCollector) RequestStop() {
	close(bc.stopChan)
	bc.isRunning = false
}

// containsIgnoreCase checks if any of the patterns exist in the text (case insensitive)
func containsIgnoreCase(text string, patterns ...string) bool {
	textLower := strings.ToLower(text)
	for _, pattern := range patterns {
		if strings.Contains(textLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}