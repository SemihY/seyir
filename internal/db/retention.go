package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"seyir/internal/logger"
	"sync"
	"time"
)

// RetentionManager manages automatic cleanup of old log files
type RetentionManager struct {
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	config             *RetentionConfig
	ticker             *time.Ticker
	lastCleanup        time.Time
	totalFilesDeleted  int64
	totalBytesDeleted  int64
	mutex              sync.RWMutex
}

// RetentionConfig holds retention policy configuration
type RetentionConfig struct {
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	RetentionDays    int           `yaml:"retention_days" json:"retention_days"`
	CleanupInterval  time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	MaxFilesPerScan  int           `yaml:"max_files_per_scan" json:"max_files_per_scan"`
	DryRun           bool          `yaml:"dry_run" json:"dry_run"`
	KeepMinFiles     int           `yaml:"keep_min_files" json:"keep_min_files"`
	MaxTotalSizeGB   float64       `yaml:"max_total_size_gb" json:"max_total_size_gb"`
}

// DefaultRetentionConfig returns sensible defaults for retention
func DefaultRetentionConfig() *RetentionConfig {
	return &RetentionConfig{
		Enabled:          false, // Disabled by default
		RetentionDays:    30,    // Keep 30 days
		CleanupInterval:  1 * time.Hour, // Check every hour
		MaxFilesPerScan:  1000,  // Don't scan too many files at once
		DryRun:           false,
		KeepMinFiles:     5,     // Always keep at least 5 files per process
		MaxTotalSizeGB:   10.0,  // Max 10GB total storage
	}
}

// RetentionStats contains statistics about retention operations
type RetentionStats struct {
	LastCleanup       time.Time `json:"last_cleanup"`
	TotalFilesDeleted int64     `json:"total_files_deleted"`
	TotalBytesDeleted int64     `json:"total_bytes_deleted"`
	NextCleanup       time.Time `json:"next_cleanup"`
	FilesScanned      int64     `json:"files_scanned"`
	ProcessDirs       int       `json:"process_dirs"`
}

var (
	globalRetentionManager *RetentionManager
	retentionManagerOnce   sync.Once
)

// GetRetentionManager returns the global retention manager
func GetRetentionManager() *RetentionManager {
	retentionManagerOnce.Do(func() {
		globalRetentionManager = NewRetentionManager(DefaultRetentionConfig())
	})
	return globalRetentionManager
}

// NewRetentionManager creates a new retention manager
func NewRetentionManager(config *RetentionConfig) *RetentionManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	rm := &RetentionManager{
		ctx:         ctx,
		cancel:      cancel,
		config:      config,
		lastCleanup: time.Now(),
	}
	
	// Start background cleanup if enabled
	if config.Enabled {
		rm.start()
	}
	
	return rm
}

// start begins the background retention cleanup process
func (rm *RetentionManager) start() {
	rm.ticker = time.NewTicker(rm.config.CleanupInterval)
	rm.wg.Add(1)
	
	go func() {
		defer rm.wg.Done()
		defer rm.ticker.Stop()
		
		if rm.config.Enabled {
			logger.Info("Retention manager started (retention: %d days, interval: %v)", 
				rm.config.RetentionDays, rm.config.CleanupInterval)
		}
		
		// Run initial cleanup after a short delay
		time.Sleep(30 * time.Second)
		rm.performCleanup()
		
		for {
			select {
			case <-rm.ticker.C:
				rm.performCleanup()
			case <-rm.ctx.Done():
				logger.Info("Retention manager shutting down")
				return
			}
		}
	}()
}

// performCleanup performs the actual cleanup operation
func (rm *RetentionManager) performCleanup() {
	start := time.Now()
	
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		logger.Warn("No lake directory set, skipping retention cleanup")
		return
	}
	
	logger.Info("Starting retention cleanup (retention: %d days)", rm.config.RetentionDays)
	
	stats := &RetentionStats{
		LastCleanup: start,
		NextCleanup: start.Add(rm.config.CleanupInterval),
	}
	
	// Get all process directories
	processDirs, err := rm.getProcessDirectories(lakeDir)
	if err != nil {
		logger.Error("Failed to get process directories: %v", err)
		return
	}
	
	stats.ProcessDirs = len(processDirs)
	
	// Process each directory
	for _, processDir := range processDirs {
		dirStats := rm.cleanupProcessDirectory(processDir)
		stats.FilesScanned += dirStats.FilesScanned
		stats.TotalFilesDeleted += dirStats.TotalFilesDeleted
		stats.TotalBytesDeleted += dirStats.TotalBytesDeleted
	}
	
	// Update global stats
	rm.mutex.Lock()
	rm.lastCleanup = start
	rm.totalFilesDeleted += stats.TotalFilesDeleted
	rm.totalBytesDeleted += stats.TotalBytesDeleted
	rm.mutex.Unlock()
	
	duration := time.Since(start)
	
	if stats.TotalFilesDeleted > 0 {
		logger.Info("Retention cleanup completed in %v: deleted %d files (%.2f MB) from %d process directories",
			duration, stats.TotalFilesDeleted, float64(stats.TotalBytesDeleted)/(1024*1024), stats.ProcessDirs)
	} else {
		logger.Debug("Retention cleanup completed in %v: no files to delete", duration)
	}
}

// getProcessDirectories returns all process directories in the lake directory
func (rm *RetentionManager) getProcessDirectories(lakeDir string) ([]string, error) {
	entries, err := os.ReadDir(lakeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read lake directory: %v", err)
	}
	
	var processDirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			processDirs = append(processDirs, filepath.Join(lakeDir, entry.Name()))
		}
	}
	
	return processDirs, nil
}

// cleanupProcessDirectory cleans up old files in a process directory
func (rm *RetentionManager) cleanupProcessDirectory(processDir string) *RetentionStats {
	stats := &RetentionStats{}
	
	// Get all parquet files in the directory
	pattern := filepath.Join(processDir, "*.parquet")
	files, err := filepath.Glob(pattern)
	if err != nil {
		logger.Error("Failed to glob files in %s: %v", processDir, err)
		return stats
	}
	
	if len(files) == 0 {
		return stats
	}
	
	stats.FilesScanned = int64(len(files))
	
	// Get file info with timestamps
	type fileInfo struct {
		path     string
		modTime  time.Time
		size     int64
	}
	
	var fileInfos []fileInfo
	var totalSize int64
	
	for _, file := range files {
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		
		fileInfos = append(fileInfos, fileInfo{
			path:    file,
			modTime: stat.ModTime(),
			size:    stat.Size(),
		})
		totalSize += stat.Size()
	}
	
	// Sort by modification time (oldest first)
	for i := 0; i < len(fileInfos)-1; i++ {
		for j := i + 1; j < len(fileInfos); j++ {
			if fileInfos[i].modTime.After(fileInfos[j].modTime) {
				fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
			}
		}
	}
	
	cutoffTime := time.Now().AddDate(0, 0, -rm.config.RetentionDays)
	
	// Determine files to delete
	var filesToDelete []fileInfo
	
	for i, fi := range fileInfos {
		// Keep minimum number of files
		remainingFiles := len(fileInfos) - i
		if remainingFiles <= rm.config.KeepMinFiles {
			break
		}
		
		// Check retention policy
		if fi.modTime.Before(cutoffTime) {
			filesToDelete = append(filesToDelete, fi)
		}
	}
	
	// Also check total size limit
	if rm.config.MaxTotalSizeGB > 0 {
		maxBytes := int64(rm.config.MaxTotalSizeGB * 1024 * 1024 * 1024)
		if totalSize > maxBytes {
			// Need to delete more files to stay under size limit
			bytesToDelete := totalSize - maxBytes
			
			for _, fi := range fileInfos {
				if bytesToDelete <= 0 {
					break
				}
				
				// Check if already in delete list
				alreadyDeleting := false
				for _, fd := range filesToDelete {
					if fd.path == fi.path {
						alreadyDeleting = true
						break
					}
				}
				
				if !alreadyDeleting {
					// Keep minimum files rule
					remainingFiles := len(fileInfos) - len(filesToDelete) - 1
					if remainingFiles >= rm.config.KeepMinFiles {
						filesToDelete = append(filesToDelete, fi)
						bytesToDelete -= fi.size
					}
				}
			}
		}
	}
	
	// Delete files
	for _, fi := range filesToDelete {
		if rm.config.DryRun {
			logger.Info("[DRY-RUN] Would delete: %s (%.2f MB, %s old)",
				filepath.Base(fi.path), float64(fi.size)/(1024*1024), time.Since(fi.modTime).Truncate(time.Hour))
		} else {
			if err := os.Remove(fi.path); err != nil {
				logger.Error("Failed to delete %s: %v", fi.path, err)
			} else {
				logger.Debug("Deleted: %s (%.2f MB, %s old)",
					filepath.Base(fi.path), float64(fi.size)/(1024*1024), time.Since(fi.modTime).Truncate(time.Hour))
				stats.TotalFilesDeleted++
				stats.TotalBytesDeleted += fi.size
			}
		}
	}
	
	return stats
}

// UpdateConfig updates the retention configuration
func (rm *RetentionManager) UpdateConfig(config *RetentionConfig) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()
	
	wasEnabled := rm.config.Enabled
	rm.config = config
	
	// Restart if enabling or interval changed
	if config.Enabled && !wasEnabled {
		rm.start()
	} else if !config.Enabled && wasEnabled {
		rm.stop()
	} else if config.Enabled && rm.ticker != nil {
		// Update ticker interval if changed
		rm.ticker.Reset(config.CleanupInterval)
	}
	
	logger.Info("Retention configuration updated: enabled=%t, days=%d, interval=%v",
		config.Enabled, config.RetentionDays, config.CleanupInterval)
}

// GetConfig returns the current retention configuration
func (rm *RetentionManager) GetConfig() *RetentionConfig {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()
	
	// Return a copy
	config := *rm.config
	return &config
}

// GetStats returns retention statistics
func (rm *RetentionManager) GetStats() RetentionStats {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()
	
	nextCleanup := rm.lastCleanup.Add(rm.config.CleanupInterval)
	if !rm.config.Enabled {
		nextCleanup = time.Time{}
	}
	
	return RetentionStats{
		LastCleanup:       rm.lastCleanup,
		TotalFilesDeleted: rm.totalFilesDeleted,
		TotalBytesDeleted: rm.totalBytesDeleted,
		NextCleanup:       nextCleanup,
	}
}

// RunCleanupNow forces an immediate cleanup operation
func (rm *RetentionManager) RunCleanupNow() {
	if !rm.config.Enabled {
		logger.Warn("Retention is disabled, cannot run cleanup")
		return
	}
	
	logger.Info("Running manual retention cleanup")
	rm.performCleanup()
}

// stop stops the retention manager
func (rm *RetentionManager) stop() {
	if rm.ticker != nil {
		rm.ticker.Stop()
		rm.ticker = nil
	}
}

// Shutdown gracefully shuts down the retention manager
func (rm *RetentionManager) Shutdown() {
	logger.Info("Shutting down retention manager...")
	
	rm.cancel()
	rm.stop()
	rm.wg.Wait()
	
	logger.Info("Retention manager shutdown complete")
}

// ForceCleanupOlderThan immediately deletes files older than the specified duration
func (rm *RetentionManager) ForceCleanupOlderThan(duration time.Duration) (int64, int64, error) {
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return 0, 0, fmt.Errorf("no lake directory set")
	}
	
	logger.Info("Force cleanup: deleting files older than %v", duration)
	
	cutoffTime := time.Now().Add(-duration)
	var totalFiles, totalBytes int64
	
	processDirs, err := rm.getProcessDirectories(lakeDir)
	if err != nil {
		return 0, 0, err
	}
	
	for _, processDir := range processDirs {
		pattern := filepath.Join(processDir, "*.parquet")
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		
		for _, file := range files {
			stat, err := os.Stat(file)
			if err != nil {
				continue
			}
			
			if stat.ModTime().Before(cutoffTime) {
				if err := os.Remove(file); err != nil {
					logger.Error("Failed to delete %s: %v", file, err)
				} else {
					totalFiles++
					totalBytes += stat.Size()
					logger.Debug("Force deleted: %s", filepath.Base(file))
				}
			}
		}
	}
	
	logger.Info("Force cleanup completed: deleted %d files (%.2f MB)", 
		totalFiles, float64(totalBytes)/(1024*1024))
	
	return totalFiles, totalBytes, nil
}