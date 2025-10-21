package collector

import (
	"context"
	"seyir/internal/db"
	"seyir/internal/logger"
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
	ulLogger, err := manager.GetOrCreateLogger(processName)
	if err != nil {
		logger.Error("Failed to get/create UltraLight logger: %v", err)
		return
	}
	
	if err := ulLogger.AddEntry(entry); err != nil {
		logger.Error("UltraLight logger failed to add entry: %v", err)
		return
	}
	
	tail.BroadcastLog(entry)
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