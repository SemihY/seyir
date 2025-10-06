package db

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// BatchBufferManager provides centralized management of batch buffers
type BatchBufferManager struct {
	defaultConfig *BatchConfig
}

// NewBatchBufferManager creates a new batch buffer manager
func NewBatchBufferManager() *BatchBufferManager {
	return &BatchBufferManager{
		defaultConfig: DefaultBatchConfig(),
	}
}

// SetDefaultConfig updates the default batch configuration
func (bbm *BatchBufferManager) SetDefaultConfig(config *BatchConfig) {
	bbm.defaultConfig = config
}

// GetDefaultConfig returns the current default configuration
func (bbm *BatchBufferManager) GetDefaultConfig() *BatchConfig {
	return bbm.defaultConfig
}

// CreateCustomBatchBuffer creates a batch buffer with custom configuration
func (bbm *BatchBufferManager) CreateCustomBatchBuffer(db *DB, config *BatchConfig) *BatchBuffer {
	if config == nil {
		config = bbm.defaultConfig
	}
	return NewBatchBuffer(db, config)
}

// Global batch buffer manager instance
var globalBatchManager = NewBatchBufferManager()

// ConfigureBatchBuffers allows configuration of batch buffer settings
func ConfigureBatchBuffers(flushInterval time.Duration, batchSize int, maxMemory int64, enableAsync bool) {
	config := &BatchConfig{
		FlushInterval: flushInterval,
		BatchSize:     batchSize,
		MaxMemory:     maxMemory,
		EnableAsync:   enableAsync,
	}
	globalBatchManager.SetDefaultConfig(config)
	log.Printf("[INFO] Batch buffer configuration updated: %+v", config)
}

// GetBatchBufferStats returns statistics for all active batch buffers
func GetBatchBufferStats() map[string]BatchStats {
	return GetAllProcessBufferStats()
}

// FlushAllBuffers forces a flush of all active batch buffers
func FlushAllBuffers() error {
	buffersMutex.RLock()
	defer buffersMutex.RUnlock()
	
	var errors []string
	for processName, buffer := range processBuffers {
		if err := buffer.Flush(); err != nil {
			errors = append(errors, fmt.Sprintf("Process %s: %v", processName, err))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("flush errors: %v", errors)
	}
	
	return nil
}

// HTTP handlers for batch buffer management

// HandleBatchStats returns batch buffer statistics as JSON
func HandleBatchStats(w http.ResponseWriter, r *http.Request) {
	stats := GetBatchBufferStats()
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, "Failed to encode stats", http.StatusInternalServerError)
		return
	}
}

// HandleBatchFlush forces a flush of all batch buffers
func HandleBatchFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if err := FlushAllBuffers(); err != nil {
		http.Error(w, fmt.Sprintf("Flush failed: %v", err), http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "All buffers flushed"})
}

// HandleBatchConfig allows configuration of batch buffer settings via HTTP
func HandleBatchConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return current configuration
		config := globalBatchManager.GetDefaultConfig()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
		
	case http.MethodPost:
		// Update configuration
		var config BatchConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		
		// Validate configuration
		if config.FlushInterval < time.Second {
			http.Error(w, "FlushInterval must be at least 1 second", http.StatusBadRequest)
			return
		}
		if config.BatchSize < 1 {
			http.Error(w, "BatchSize must be at least 1", http.StatusBadRequest)
			return
		}
		if config.MaxMemory < 1024*1024 {
			http.Error(w, "MaxMemory must be at least 1MB", http.StatusBadRequest)
			return
		}
		
		globalBatchManager.SetDefaultConfig(&config)
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Configuration updated"})
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleBatchConfigQuery allows setting config via query parameters
func HandleBatchConfigQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	query := r.URL.Query()
	config := *globalBatchManager.GetDefaultConfig() // Copy current config
	
	// Parse query parameters
	if flushInterval := query.Get("flush_interval"); flushInterval != "" {
		if seconds, err := strconv.Atoi(flushInterval); err == nil && seconds >= 1 {
			config.FlushInterval = time.Duration(seconds) * time.Second
		} else {
			http.Error(w, "Invalid flush_interval (must be >= 1 second)", http.StatusBadRequest)
			return
		}
	}
	
	if batchSize := query.Get("batch_size"); batchSize != "" {
		if size, err := strconv.Atoi(batchSize); err == nil && size >= 1 {
			config.BatchSize = size
		} else {
			http.Error(w, "Invalid batch_size (must be >= 1)", http.StatusBadRequest)
			return
		}
	}
	
	if maxMemory := query.Get("max_memory_mb"); maxMemory != "" {
		if mb, err := strconv.ParseInt(maxMemory, 10, 64); err == nil && mb >= 1 {
			config.MaxMemory = mb * 1024 * 1024 // Convert MB to bytes
		} else {
			http.Error(w, "Invalid max_memory_mb (must be >= 1)", http.StatusBadRequest)
			return
		}
	}
	
	if enableAsync := query.Get("enable_async"); enableAsync != "" {
		if async, err := strconv.ParseBool(enableAsync); err == nil {
			config.EnableAsync = async
		} else {
			http.Error(w, "Invalid enable_async (must be true or false)", http.StatusBadRequest)
			return
		}
	}
	
	globalBatchManager.SetDefaultConfig(&config)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Configuration updated",
		"config":  config,
	})
}

// RegisterBatchHandlers registers HTTP handlers for batch buffer management
func RegisterBatchHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/batch/stats", HandleBatchStats)
	mux.HandleFunc("/api/batch/flush", HandleBatchFlush)
	mux.HandleFunc("/api/batch/config", HandleBatchConfig)
	mux.HandleFunc("/api/batch/configure", HandleBatchConfigQuery)
}

// BatchBufferHealthCheck checks the health of batch buffers
func BatchBufferHealthCheck() map[string]interface{} {
	stats := GetBatchBufferStats()
	
	totalBuffers := len(stats)
	totalErrors := int64(0)
	oldestFlush := time.Now()
	
	for _, stat := range stats {
		totalErrors += stat.FlushErrors
		if stat.LastFlush.Before(oldestFlush) {
			oldestFlush = stat.LastFlush
		}
	}
	
	// Get async flush manager stats
	var asyncStats AsyncFlushStats
	if globalAsyncFlushManager != nil {
		asyncStats = globalAsyncFlushManager.GetStats()
		totalErrors += asyncStats.TotalErrors
	}
	
	health := map[string]interface{}{
		"total_buffers":     totalBuffers,
		"total_errors":      totalErrors,
		"oldest_flush":      oldestFlush,
		"time_since_flush":  time.Since(oldestFlush),
		"healthy":           totalBuffers > 0 && totalErrors == 0 && time.Since(oldestFlush) < 1*time.Minute,
		"async_flush_stats": asyncStats,
	}
	
	return health
}

// CleanupBatchBuffers provides a cleanup mechanism for batch buffers
func CleanupBatchBuffers() {
	log.Printf("[INFO] Starting batch buffer cleanup...")
	
	// Close all process buffers
	CloseAllProcessBuffers()
	
	log.Printf("[INFO] Batch buffer cleanup completed")
}