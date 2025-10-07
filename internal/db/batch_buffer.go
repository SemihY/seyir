package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BatchConfig holds configuration for the batch buffer
type BatchConfig struct {
	FlushInterval time.Duration // Flush interval (e.g., 5 seconds)
	BatchSize     int           // Batch size (e.g., 10,000 logs)
	MaxMemory     int64         // Maximum memory usage in bytes
	EnableAsync   bool          // Enable async flush operations
}

// DefaultBatchConfig returns sensible defaults
func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		FlushInterval: 5 * time.Second,
		BatchSize:     10000,
		MaxMemory:     100 * 1024 * 1024, // 100MB
		EnableAsync:   true,
	}
}

// BatchBuffer manages logs in RAM before flushing to disk
type BatchBuffer struct {
	config       *BatchConfig
	buffer       []*LogEntry
	bufferMutex  sync.RWMutex
	db          *DB
	
	// Timing and control
	lastFlush   time.Time
	flushTicker *time.Ticker
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	
	// Metrics
	totalLogs    int64
	totalFlushes int64
	flushErrors  int64
	
	// File rotation
	processDir   string
	currentFile  string
	fileRotation *FileRotation
}

// FileRotation manages file rotation per process
type FileRotation struct {
	processName    string
	baseDir        string
	maxFileSize    int64
	maxFiles       int
	compression    string // Compression type: "zstd", "snappy", "gzip", "uncompressed"
	currentSize    int64
	currentFileNum int
	currentFile    string  // Cache current file path
	mutex          sync.Mutex
}

// NewFileRotation creates a new file rotation manager
func NewFileRotation(processName, baseDir string, maxFileSize int64, maxFiles int, compression string) *FileRotation {
	return &FileRotation{
		processName:    processName,
		baseDir:        baseDir,
		maxFileSize:    maxFileSize,
		maxFiles:       maxFiles,
		compression:    compression,
		currentFileNum: 1,
	}
}

// GetCurrentFilePath returns the current file path for writing
func (fr *FileRotation) GetCurrentFilePath() (string, error) {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()
	
	// If we don't have a current file, generate one
	if fr.currentFile == "" {
		if err := fr.generateNewFilePath(); err != nil {
			return "", err
		}
	}
	
	return fr.currentFile, nil
}

// generateNewFilePath creates a new file path (internal use only)
func (fr *FileRotation) generateNewFilePath() error {
	// Create process directory if it doesn't exist
	processDir := filepath.Join(fr.baseDir, fr.processName)
	if err := os.MkdirAll(processDir, 0755); err != nil {
		return fmt.Errorf("failed to create process directory %s: %v", processDir, err)
	}
	
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s_%d.parquet", fr.processName, timestamp, fr.currentFileNum)
	fr.currentFile = filepath.Join(processDir, filename)
	
	return nil
}

// ShouldRotate checks if we need to rotate the file
func (fr *FileRotation) ShouldRotate(additionalSize int64) bool {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()
	
	return fr.currentSize+additionalSize > fr.maxFileSize
}

// Rotate creates a new file for writing
func (fr *FileRotation) Rotate() error {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()
	
	fr.currentFileNum++
	fr.currentSize = 0
	fr.currentFile = "" // Clear current file to force new generation
	
	// Clean up old files if we exceed maxFiles
	if fr.maxFiles > 0 {
		return fr.cleanupOldFiles()
	}
	
	return nil
}

// AddSize updates the current file size
func (fr *FileRotation) AddSize(size int64) {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()
	fr.currentSize += size
}

// cleanupOldFiles removes old files if we exceed the limit
func (fr *FileRotation) cleanupOldFiles() error {
	processDir := filepath.Join(fr.baseDir, fr.processName)
	
	// Get all parquet files in the process directory
	pattern := filepath.Join(processDir, "*.parquet")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to list files for cleanup: %v", err)
	}
	
	// If we don't exceed the limit, no cleanup needed
	if len(files) <= fr.maxFiles {
		return nil
	}
	
	// Sort files by modification time (oldest first)
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	
	var fileInfos []fileInfo
	for _, file := range files {
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{
			path:    file,
			modTime: stat.ModTime(),
		})
	}
	
	// Remove oldest files
	filesToRemove := len(fileInfos) - fr.maxFiles
	for i := 0; i < filesToRemove; i++ {
		oldestFile := fileInfos[0]
		for j := 1; j < len(fileInfos); j++ {
			if fileInfos[j].modTime.Before(oldestFile.modTime) {
				oldestFile = fileInfos[j]
			}
		}
		
		log.Printf("[INFO] Removing old log file: %s", oldestFile.path)
		if err := os.Remove(oldestFile.path); err != nil {
			log.Printf("[ERROR] Failed to remove old file %s: %v", oldestFile.path, err)
		}
		
		// Remove from slice
		for j := 0; j < len(fileInfos); j++ {
			if fileInfos[j].path == oldestFile.path {
				fileInfos = append(fileInfos[:j], fileInfos[j+1:]...)
				break
			}
		}
	}
	
	return nil
}

// NewBatchBuffer creates a new batch buffer with the given configuration
func NewBatchBuffer(db *DB, config *BatchConfig) *BatchBuffer {
	if config == nil {
		config = DefaultBatchConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Determine process name for file rotation
	processName := fmt.Sprintf("seyir_pid_%d", os.Getpid())
	if cmd := os.Args[0]; cmd != "" {
		processName = fmt.Sprintf("%s_pid_%d", filepath.Base(cmd), os.Getpid())
	}
	
	// Create file rotation manager
	lakeDir := GetGlobalLakeDir()
	
	// Get config values from config file
	configFile, err := LoadConfigFromFile(DefaultConfigPath)
	if err != nil {
		log.Printf("[WARN] Could not load config file, using defaults: %v", err)
		configFile = DefaultConfigFile()
	}
	
	fileRotation := NewFileRotation(
		processName,
		lakeDir,
		int64(configFile.FileRotation.MaxFileSizeMB)*1024*1024, // Config'den al
		configFile.FileRotation.MaxFiles,                       // Config'den al
		configFile.FileRotation.Compression,                    // Config'den al
	)
	
	bb := &BatchBuffer{
		config:       config,
		buffer:       make([]*LogEntry, 0, config.BatchSize),
		db:          db,
		lastFlush:   time.Now(),
		ctx:         ctx,
		cancel:      cancel,
		processDir:  filepath.Join(lakeDir, processName),
		fileRotation: fileRotation,
	}
	
	// Initialize async flush manager (if not already initialized)
	GetAsyncFlushManager()
	
	// Start the background flush ticker if async is enabled
	if config.EnableAsync {
		bb.startFlushTicker()
	}
	
	return bb
}

// startFlushTicker starts the background flush ticker
func (bb *BatchBuffer) startFlushTicker() {
	bb.flushTicker = time.NewTicker(bb.config.FlushInterval)
	bb.wg.Add(1)
	
	go func() {
		defer bb.wg.Done()
		for {
			select {
			case <-bb.flushTicker.C:
				if err := bb.FlushIfNeeded(); err != nil {
					log.Printf("[ERROR] Background flush failed: %v", err)
					bb.flushErrors++
				}
			case <-bb.ctx.Done():
				return
			}
		}
	}()
}

// Add adds a log entry to the buffer
func (bb *BatchBuffer) Add(entry *LogEntry) error {
	bb.bufferMutex.Lock()
	defer bb.bufferMutex.Unlock()
	
	bb.buffer = append(bb.buffer, entry)
	bb.totalLogs++
	
	// Check if we need to flush immediately
	shouldFlush := len(bb.buffer) >= bb.config.BatchSize ||
		time.Since(bb.lastFlush) >= bb.config.FlushInterval
	
	if shouldFlush {
		if bb.config.EnableAsync {
			// Async flush
			go func() {
				if err := bb.FlushIfNeeded(); err != nil {
					log.Printf("[ERROR] Async flush failed: %v", err)
					bb.flushErrors++
				}
			}()
		} else {
			// Synchronous flush
			return bb.flush()
		}
	}
	
	return nil
}

// FlushIfNeeded flushes the buffer if conditions are met
func (bb *BatchBuffer) FlushIfNeeded() error {
	bb.bufferMutex.RLock()
	shouldFlush := len(bb.buffer) > 0 && (
		len(bb.buffer) >= bb.config.BatchSize ||
		time.Since(bb.lastFlush) >= bb.config.FlushInterval)
	bb.bufferMutex.RUnlock()
	
	if shouldFlush {
		return bb.flush()
	}
	
	return nil
}

// Flush forces a flush of the buffer
func (bb *BatchBuffer) Flush() error {
	return bb.flush()
}

// flush performs the actual flush operation using async flush manager
func (bb *BatchBuffer) flush() error {
	bb.bufferMutex.Lock()
	
	if len(bb.buffer) == 0 {
		bb.bufferMutex.Unlock()
		return nil
	}
	
	// Create a copy of the buffer to work with
	entries := make([]*LogEntry, len(bb.buffer))
	copy(entries, bb.buffer)
	
	// Clear the buffer early to allow new entries while we're flushing
	bb.buffer = bb.buffer[:0]
	bb.bufferMutex.Unlock()
	
	log.Printf("[DEBUG] Preparing to flush %d entries", len(entries))
	
	// Check if we need to rotate files
	estimatedSize := int64(len(entries) * 1024) // Rough estimate
	if bb.fileRotation.ShouldRotate(estimatedSize) {
		if err := bb.fileRotation.Rotate(); err != nil {
			log.Printf("[ERROR] File rotation failed: %v", err)
		}
	}
	
	// Get current file path
	filePath, err := bb.fileRotation.GetCurrentFilePath()
	if err != nil {
		return fmt.Errorf("failed to get file path: %v", err)
	}
	
	// Submit async flush task
	asyncManager := GetAsyncFlushManager()
	task := &FlushTask{
		Buffer:     bb,
		Entries:    entries,
		FilePath:   filePath,
		Priority:   0, // Normal priority
		MaxRetries: 3,
		Callback:   bb.onFlushComplete,
	}
	
	if bb.config.EnableAsync {
		// Async flush
		err = asyncManager.SubmitFlushTask(task)
		if err != nil {
			// Fallback to synchronous flush if async fails
			log.Printf("[WARN] Async flush failed, falling back to sync: %v", err)
			return bb.flushSync(entries, filePath)
		}
		
		// Update metrics immediately for async (actual completion handled in callback)
		bb.lastFlush = time.Now()
		bb.fileRotation.AddSize(estimatedSize)
		
		return nil
	} else {
		// Synchronous flush
		return bb.flushSync(entries, filePath)
	}
}

// flushSync performs synchronous flush (fallback method)
func (bb *BatchBuffer) flushSync(entries []*LogEntry, filePath string) error {
	err := bb.writeEntriesToFile(entries, filePath)
	if err != nil {
		// On error, put entries back into buffer to retry later
		bb.bufferMutex.Lock()
		bb.buffer = append(entries, bb.buffer...)
		bb.flushErrors++
		bb.bufferMutex.Unlock()
		return fmt.Errorf("failed to write entries to file: %v", err)
	}
	
	// Update metrics and timing
	bb.lastFlush = time.Now()
	bb.totalFlushes++
	estimatedSize := int64(len(entries) * 1024)
	bb.fileRotation.AddSize(estimatedSize)
	
	log.Printf("[DEBUG] Successfully flushed %d entries to %s (sync)", len(entries), filepath.Base(filePath))
	return nil
}

// onFlushComplete is called when an async flush operation completes
func (bb *BatchBuffer) onFlushComplete(err error) {
	if err != nil {
		bb.flushErrors++
		log.Printf("[ERROR] Async flush failed: %v", err)
	} else {
		bb.totalFlushes++
		log.Printf("[DEBUG] Async flush completed successfully")
	}
}

// writeEntriesToFile writes entries to a parquet file
func (bb *BatchBuffer) writeEntriesToFile(entries []*LogEntry, filePath string) error {
	// Create a temporary in-memory database for this flush operation
	tempDB, err := NewSessionConnection()
	if err != nil {
		return fmt.Errorf("failed to create temp database: %v", err)
	}
	defer tempDB.CloseSession()
	
	// Insert all entries into the temp database
	for _, entry := range entries {
		tagsArray := convertTagsToArray(entry.Tags)
		_, err := tempDB.Exec(`INSERT INTO logs (ts, source, level, message, id, trace_id, process, component, thread, user_id, request_id, tags) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.Ts, entry.Source, entry.Level, entry.Message, entry.ID, entry.TraceID, entry.Process, entry.Component, entry.Thread, entry.UserID, entry.RequestID, tagsArray)
		if err != nil {
			return fmt.Errorf("failed to insert entry: %v", err)
		}
	}
	
	// Check if file exists (for append mode)
	fileExists := false
	if _, err := os.Stat(filePath); err == nil {
		fileExists = true
	}
	
	if fileExists {
		// Read existing data and merge
		_, err = tempDB.Exec("CREATE TEMPORARY TABLE existing_logs AS SELECT * FROM read_parquet(?)", filePath)
		if err != nil {
			return fmt.Errorf("failed to read existing parquet data: %v", err)
		}
		
		// Insert existing data into our logs table
		_, err = tempDB.Exec("INSERT INTO logs SELECT * FROM existing_logs")
		if err != nil {
			return fmt.Errorf("failed to merge existing data: %v", err)
		}
		
		// Drop temporary table
		tempDB.Exec("DROP TABLE existing_logs")
	}
	
	// Export to parquet with compression from config
	compression := bb.fileRotation.compression
	if compression == "" {
		compression = "zstd" // Default to ZSTD if not specified
	}
	exportSQL := fmt.Sprintf("COPY logs TO '%s' (FORMAT PARQUET, COMPRESSION '%s')", filePath, strings.ToUpper(compression))
	_, err = tempDB.Exec(exportSQL)
	if err != nil {
		return fmt.Errorf("failed to export to parquet: %v", err)
	}
	
	return nil
}

// GetStats returns buffer statistics
func (bb *BatchBuffer) GetStats() BatchStats {
	bb.bufferMutex.RLock()
	defer bb.bufferMutex.RUnlock()
	
	return BatchStats{
		BufferSize:     len(bb.buffer),
		BufferCapacity: cap(bb.buffer),
		TotalLogs:      bb.totalLogs,
		TotalFlushes:   bb.totalFlushes,
		FlushErrors:    bb.flushErrors,
		LastFlush:      bb.lastFlush,
		ProcessDir:     bb.processDir,
	}
}

// BatchStats contains statistics about the batch buffer
type BatchStats struct {
	BufferSize     int       `json:"buffer_size"`
	BufferCapacity int       `json:"buffer_capacity"`
	TotalLogs      int64     `json:"total_logs"`
	TotalFlushes   int64     `json:"total_flushes"`
	FlushErrors    int64     `json:"flush_errors"`
	LastFlush      time.Time `json:"last_flush"`
	ProcessDir     string    `json:"process_dir"`
}

// Close stops the batch buffer and flushes any remaining entries
func (bb *BatchBuffer) Close() error {
	// Stop the ticker
	if bb.flushTicker != nil {
		bb.flushTicker.Stop()
	}
	
	// Cancel context to stop background goroutines
	bb.cancel()
	
	// Wait for background goroutines to finish
	bb.wg.Wait()
	
	// Flush any remaining entries
	if err := bb.flush(); err != nil {
		log.Printf("[ERROR] Final flush failed: %v", err)
		return err
	}
	
	log.Printf("[INFO] BatchBuffer closed. Final stats: %+v", bb.GetStats())
	return nil
}

// ProcessBuffers manages batch buffers for different processes
var (
	processBuffers = make(map[string]*BatchBuffer)
	buffersMutex   sync.RWMutex
)

// GetOrCreateProcessBuffer gets or creates a batch buffer for a process
func GetOrCreateProcessBuffer(processName string, db *DB, config *BatchConfig) *BatchBuffer {
	buffersMutex.Lock()
	defer buffersMutex.Unlock()
	
	if buffer, exists := processBuffers[processName]; exists {
		return buffer
	}
	
	buffer := NewBatchBuffer(db, config)
	processBuffers[processName] = buffer
	
	log.Printf("[INFO] Created new batch buffer for process: %s", processName)
	return buffer
}

// CloseAllProcessBuffers closes all process buffers and async flush manager
func CloseAllProcessBuffers() {
	buffersMutex.Lock()
	defer buffersMutex.Unlock()
	
	for processName, buffer := range processBuffers {
		log.Printf("[INFO] Closing batch buffer for process: %s", processName)
		if err := buffer.Close(); err != nil {
			log.Printf("[ERROR] Failed to close buffer for process %s: %v", processName, err)
		}
	}
	
	// Clear the map
	processBuffers = make(map[string]*BatchBuffer)
	
	// Shutdown async flush manager
	if globalAsyncFlushManager != nil {
		globalAsyncFlushManager.Shutdown()
	}
}

// GetAllProcessBufferStats returns statistics for all process buffers
func GetAllProcessBufferStats() map[string]BatchStats {
	buffersMutex.RLock()
	defer buffersMutex.RUnlock()
	
	stats := make(map[string]BatchStats)
	for processName, buffer := range processBuffers {
		stats[processName] = buffer.GetStats()
	}
	
	return stats
}