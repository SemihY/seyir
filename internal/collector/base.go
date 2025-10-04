package collector

import (
	"context"
	"log"
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
	
	// Close closes any resources (like database connections)
	Close() error
	
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
// Each collector gets its own DuckDB session file (e.g., logs_session_123.duckdb)
func NewBaseCollector(sourceName string) *BaseCollector {
	// Create a new DuckDB session for this collector
	database, err := db.NewSessionConnection()
	if err != nil {
		log.Printf("[ERROR] Failed to create session connection for %s: %v", sourceName, err)
		return nil
	}
	
	return &BaseCollector{
		database:   database,
		sourceName: sourceName,
		stopChan:   make(chan struct{}),
	}
}

// SaveAndBroadcast saves a log entry and broadcasts it to connected clients
func (bc *BaseCollector) SaveAndBroadcast(entry *db.LogEntry) {
	// Save directly to Parquet for immediate persistence
	if err := bc.database.SaveLogDirectly(entry); err != nil {
		log.Printf("[ERROR] Failed to save log to Parquet: %v", err)
		// Fallback: save to memory table
		db.SaveLog(bc.database, entry)
	}
	
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

// Close closes the database connection and cleans up the session
func (bc *BaseCollector) Close() error {
	if bc.database != nil {
		return bc.database.CloseSession()
	}
	return nil
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