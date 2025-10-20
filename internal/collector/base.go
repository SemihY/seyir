package collector

import (
	"context"
	"log"
	"seyir/internal/db"
	"seyir/internal/tail"
)

// LogSource represents a source of logs (stdin, docker, file, etc.)
type LogSource interface {
	// Start begins collecting logs from this source
	Start(ctx context.Context) error
	
	// Stop gracefully stops log collection
	Stop() error
	
	// Close closes any resources
	Close() error
	
	// Name returns a human-readable name for this source
	Name() string
	
	// IsHealthy returns true if the source is functioning properly
	IsHealthy() bool
}

// BaseCollector provides common functionality for all log sources
type BaseCollector struct {
	sourceName string
	isRunning  bool
	stopChan   chan struct{}
}

// NewBaseCollector creates a new base collector
func NewBaseCollector(sourceName string) *BaseCollector {
	return &BaseCollector{
		sourceName: sourceName,
		stopChan:   make(chan struct{}),
	}
}

// SaveAndBroadcast saves a log entry and broadcasts it to connected clients
func (bc *BaseCollector) SaveAndBroadcast(entry *db.LogEntry) {
	processName := entry.Source
	if processName == "" {
		processName = bc.sourceName
	}
	
	manager := db.GetUltraLightLoggerManager()
	logger, err := manager.GetOrCreateLogger(processName)
	if err != nil {
		log.Printf("[ERROR] Failed to get/create UltraLight logger: %v", err)
		return
	}
	
	if err := logger.AddEntry(entry); err != nil {
		log.Printf("[ERROR] UltraLight logger failed to add entry: %v", err)
		return
	}
	
	tail.BroadcastLog(entry)
}

// ParseLogLevel attempts to detect the log level from the message
func (bc *BaseCollector) ParseLogLevel(message string) db.Level {
	msg := message // Simple conversion for now
	_ = msg
	
	// Check for common log level patterns
	for _, pattern := range []string{"[ERROR]", "ERROR:", "error:", "FATAL", "fatal"} {
		if contains(message, pattern) {
			return db.ERROR
		}
	}
	for _, pattern := range []string{"[WARN]", "WARN:", "warn:", "WARNING", "warning"} {
		if contains(message, pattern) {
			return db.WARN
		}
	}
	for _, pattern := range []string{"[DEBUG]", "DEBUG:", "debug:", "TRACE", "trace"} {
		if contains(message, pattern) {
			return db.DEBUG
		}
	}
	return db.INFO
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

// contains checks if text contains pattern (case-sensitive for performance)
func contains(text, pattern string) bool {
	return len(text) >= len(pattern) && 
		(text[:len(pattern)] == pattern || 
		 len(text) > len(pattern) && findInString(text, pattern))
}

func findInString(text, pattern string) bool {
	for i := 0; i <= len(text)-len(pattern); i++ {
		if text[i:i+len(pattern)] == pattern {
			return true
		}
	}
	return false
}