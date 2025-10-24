package db

import (
	"fmt"
	"os"
	"path/filepath"
	"seyir/internal/config"
	"seyir/internal/logger"
	"sync"
	"time"
)

// UltraLightLoggerManager manages ultra-lightweight loggers for multiple processes
type UltraLightLoggerManager struct {
	loggers map[string]*UltraLightLogger
	mutex   sync.RWMutex
	config  *UltraLightConfig
}

// UltraLightConfig contains configuration for ultra-lightweight logging
type UltraLightConfig struct {
	Enabled        bool
	BufferSize     int
	ExportInterval int // seconds
	MaxMemoryMB    int
}

var (
	globalUltraLightManager *UltraLightLoggerManager
	ultraLightMutex         sync.Mutex
)

// GetUltraLightLoggerManager returns the global ultra-lightweight logger manager
func GetUltraLightLoggerManager() *UltraLightLoggerManager {
	ultraLightMutex.Lock()
	defer ultraLightMutex.Unlock()

	if globalUltraLightManager == nil {
		// Load configuration
		cfg := config.Get()

		globalUltraLightManager = &UltraLightLoggerManager{
			loggers: make(map[string]*UltraLightLogger),
			config: &UltraLightConfig{
				Enabled:        true, // Always enabled in new system
				BufferSize:     cfg.Buffer.Size,
				ExportInterval: cfg.Buffer.FlushIntervalSeconds,
				MaxMemoryMB:    cfg.Buffer.MaxMemoryMB,
			},
		}

		logger.Info("Ultra-Light Logger Manager initialized (enabled: %t, buffer: %d, interval: %ds)",
			globalUltraLightManager.config.Enabled,
			globalUltraLightManager.config.BufferSize,
			globalUltraLightManager.config.ExportInterval)
	}

	return globalUltraLightManager
}

// GetOrCreateLogger gets or creates an ultra-lightweight logger for a process
func (ullm *UltraLightLoggerManager) GetOrCreateLogger(processName string) (*UltraLightLogger, error) {
	ullm.mutex.Lock()
	defer ullm.mutex.Unlock()

	// Return existing logger if found
	if ulLogger, exists := ullm.loggers[processName]; exists {
		return ulLogger, nil
	}

	// Create new logger
	ulLogger := NewUltraLightLogger(
		processName,
		ullm.config.BufferSize,
		ullm.getExportInterval(),
	)

	// Start the logger
	if err := ulLogger.Start(); err != nil {
		return nil, fmt.Errorf("failed to start logger: %v", err)
	}

	ullm.loggers[processName] = ulLogger

	logger.Info("Created ultra-light logger for process: %s", processName)

	return ulLogger, nil
}

// GetLogger returns an existing logger for a process
func (ullm *UltraLightLoggerManager) GetLogger(processName string) (*UltraLightLogger, bool) {
	ullm.mutex.RLock()
	defer ullm.mutex.RUnlock()

	logger, exists := ullm.loggers[processName]
	return logger, exists
}

// StopAllLoggers stops all ultra-lightweight loggers
func (ullm *UltraLightLoggerManager) StopAllLoggers() error {
	ullm.mutex.Lock()
	defer ullm.mutex.Unlock()

	logger.Info("Stopping all ultra-light loggers (%d total)", len(ullm.loggers))

	for processName, ulLogger := range ullm.loggers {
		logger.Info("Stopping ultra-light logger for process: %s", processName)
		if err := ulLogger.Stop(); err != nil {
			logger.Error("Failed to stop logger for %s: %v", processName, err)
		}
	}

	// Clear loggers map
	ullm.loggers = make(map[string]*UltraLightLogger)

	return nil
}

// GetAllStats returns statistics for all loggers
func (ullm *UltraLightLoggerManager) GetAllStats() map[string]UltraLightStats {
	ullm.mutex.RLock()
	defer ullm.mutex.RUnlock()

	stats := make(map[string]UltraLightStats)
	for processName, logger := range ullm.loggers {
		stats[processName] = logger.GetStats()
	}

	return stats
}

// IsEnabled returns whether ultra-lightweight logging is enabled
func (ullm *UltraLightLoggerManager) IsEnabled() bool {
	return ullm.config.Enabled
}

// getExportInterval returns the export interval as a duration
func (ullm *UltraLightLoggerManager) getExportInterval() time.Duration {
	return time.Duration(ullm.config.ExportInterval) * time.Second
}

// CleanupUltraLightLoggers stops all ultra-lightweight loggers and exports remaining data
func CleanupUltraLightLoggers() error {
	manager := GetUltraLightLoggerManager()
	return manager.StopAllLoggers()
}

// Helper function to get or create logger with automatic process name detection
func GetOrCreateUltraLightLogger() (*UltraLightLogger, error) {
	manager := GetUltraLightLoggerManager()

	// Determine process name
	processName := fmt.Sprintf("seyir_pid_%d", os.Getpid())
	if cmd := os.Args[0]; cmd != "" {
		processName = fmt.Sprintf("%s_pid_%d", filepath.Base(cmd), os.Getpid())
	}

	return manager.GetOrCreateLogger(processName)
}

// GetAllUltraLightStats returns statistics for all ultra-lightweight loggers
func GetAllUltraLightStats() map[string]UltraLightStats {
	manager := GetUltraLightLoggerManager()
	return manager.GetAllStats()
}
