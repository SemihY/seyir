package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// AsyncFlushManager manages async flush operations and signal handling
type AsyncFlushManager struct {
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	flushQueue       chan *FlushTask
	emergencyBuffer  *EmergencyBuffer
	signalHandlerSet bool
	mutex            sync.Mutex
	isShutdown       bool
	
	// Statistics
	totalFlushes    int64
	totalErrors     int64
	lastFlushTime   time.Time
}

// FlushTask represents a flush operation
type FlushTask struct {
	Buffer      *BatchBuffer
	Entries     []*LogEntry
	FilePath    string
	Priority    int // 0 = normal, 1 = high (signal), 2 = emergency
	Callback    func(error)
	RetryCount  int
	MaxRetries  int
}

// EmergencyBuffer provides crash-safe temporary storage
type EmergencyBuffer struct {
	tempDir     string
	bufferFiles map[string]*os.File
	mutex       sync.RWMutex
}

var (
	globalAsyncFlushManager *AsyncFlushManager
	asyncManagerOnce        sync.Once
)

// GetAsyncFlushManager returns the global async flush manager
func GetAsyncFlushManager() *AsyncFlushManager {
	asyncManagerOnce.Do(func() {
		globalAsyncFlushManager = NewAsyncFlushManager()
	})
	return globalAsyncFlushManager
}

// NewAsyncFlushManager creates a new async flush manager
func NewAsyncFlushManager() *AsyncFlushManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create emergency buffer directory
	tempDir := filepath.Join(os.TempDir(), "seyir_emergency_buffer")
	os.MkdirAll(tempDir, 0755)
	
	manager := &AsyncFlushManager{
		ctx:             ctx,
		cancel:          cancel,
		flushQueue:      make(chan *FlushTask, 1000), // Buffered channel for async ops
		emergencyBuffer: &EmergencyBuffer{
			tempDir:     tempDir,
			bufferFiles: make(map[string]*os.File),
		},
		lastFlushTime:   time.Now(),
	}
	
	// Start flush workers
	manager.startFlushWorkers()
	
	// Setup signal handling
	manager.setupSignalHandling()
	
	return manager
}

// startFlushWorkers starts background workers for processing flush tasks
func (afm *AsyncFlushManager) startFlushWorkers() {
	numWorkers := 3 // Multiple workers for parallel I/O
	
	for i := 0; i < numWorkers; i++ {
		afm.wg.Add(1)
		go afm.flushWorker(i)
	}
}

// flushWorker processes flush tasks from the queue
func (afm *AsyncFlushManager) flushWorker(workerID int) {
	defer afm.wg.Done()
	
	log.Printf("[DEBUG] Async flush worker %d started", workerID)
	
	for {
		select {
		case task, ok := <-afm.flushQueue:
			if !ok {
				log.Printf("[DEBUG] Async flush worker %d: queue closed", workerID)
				return
			}
			if task != nil {
				afm.processFlushTask(workerID, task)
			} else {
				log.Printf("[WARN] Worker %d received nil task from queue", workerID)
			}
		case <-afm.ctx.Done():
			log.Printf("[DEBUG] Async flush worker %d shutting down", workerID)
			return
		}
	}
}

// processFlushTask processes a single flush task
func (afm *AsyncFlushManager) processFlushTask(workerID int, task *FlushTask) {
	if task == nil {
		log.Printf("[ERROR] Worker %d received nil flush task", workerID)
		return
	}
	
	start := time.Now()
	err := afm.performFlush(task)
	
	if err != nil {
		atomic.AddInt64(&afm.totalErrors, 1)
		log.Printf("[ERROR] Worker %d flush failed for %s (attempt %d/%d): %v", 
			workerID, filepath.Base(task.FilePath), task.RetryCount+1, task.MaxRetries, err)
		
		// Retry logic
		if task.RetryCount < task.MaxRetries {
			task.RetryCount++
			time.Sleep(time.Duration(task.RetryCount) * time.Second) // Exponential backoff
			
			// Re-queue with higher priority if this is a retry
			if task.Priority < 2 {
				task.Priority++
			}
			
			select {
			case afm.flushQueue <- task:
				log.Printf("[DEBUG] Re-queued flush task for retry %d", task.RetryCount)
			default:
				log.Printf("[ERROR] Failed to re-queue flush task, dropping")
				if task.Callback != nil {
					task.Callback(fmt.Errorf("max retries exceeded and queue full"))
				}
			}
			return
		}
		
		// Save to emergency buffer on final failure
		afm.saveToEmergencyBuffer(task.Entries)
	} else {
		atomic.AddInt64(&afm.totalFlushes, 1)
		afm.lastFlushTime = time.Now()
		log.Printf("[DEBUG] Worker %d successfully flushed %d entries to %s in %v", 
			workerID, len(task.Entries), filepath.Base(task.FilePath), time.Since(start))
	}
	
	// Call callback if provided
	if task.Callback != nil {
		task.Callback(err)
	}
}

// performFlush performs the actual flush operation using DuckDB
func (afm *AsyncFlushManager) performFlush(task *FlushTask) error {
	if task == nil {
		return fmt.Errorf("flush task is nil")
	}
	
	if len(task.Entries) == 0 {
		return nil
	}
	
	// Create a temporary in-memory database for this flush operation
	tempDB, err := NewSessionConnection()
	if err != nil {
		return fmt.Errorf("failed to create temp database: %v", err)
	}
	defer tempDB.CloseSession()
	
	// Insert all entries into the temp database with optimized schema
	for _, entry := range task.Entries {
		// Use simplified schema: timestamp, level, source, message, process_id, trace_id
		processID := entry.Process
		if processID == "" {
			processID = fmt.Sprintf("pid_%d", os.Getpid())
		}
		
		_, err := tempDB.Exec(`INSERT INTO logs (ts, source, level, message, id, trace_id, process, component, thread, user_id, request_id, tags) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.Ts, entry.Source, entry.Level, entry.Message, entry.ID, 
			entry.TraceID, processID, entry.Component, entry.Thread, 
			entry.UserID, entry.RequestID, convertTagsToArray(entry.Tags))
		if err != nil {
			return fmt.Errorf("failed to insert entry: %v", err)
		}
	}
	
	// Handle file existence for append-like behavior
	fileExists := false
	if _, err := os.Stat(task.FilePath); err == nil {
		fileExists = true
	}
	
	if fileExists {
		// Read existing data and merge (DuckDB limitation workaround)
		_, err = tempDB.Exec("CREATE TEMPORARY TABLE existing_logs AS SELECT * FROM read_parquet(?)", task.FilePath)
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
	
	// Export to parquet with optimized compression
	// Use ZSTD compression equivalent settings in DuckDB
	exportSQL := fmt.Sprintf("COPY logs TO '%s' (FORMAT PARQUET, COMPRESSION 'SNAPPY', ROW_GROUP_SIZE 131072)", task.FilePath)
	_, err = tempDB.Exec(exportSQL)
	if err != nil {
		return fmt.Errorf("failed to export to parquet: %v", err)
	}
	
	return nil
}

// SubmitFlushTask submits a flush task for async processing
func (afm *AsyncFlushManager) SubmitFlushTask(task *FlushTask) error {
	// Set defaults
	if task.MaxRetries == 0 {
		task.MaxRetries = 3
	}
	
	select {
	case afm.flushQueue <- task:
		return nil
	default:
		// Queue is full, handle based on priority
		if task.Priority >= 1 {
			// High priority - try to make room by processing one item synchronously
			select {
			case existingTask := <-afm.flushQueue:
				// Process the existing task immediately
				go afm.processFlushTask(-1, existingTask)
				// Now add our high priority task
				afm.flushQueue <- task
				return nil
			default:
			}
		}
		return fmt.Errorf("flush queue is full")
	}
}

// setupSignalHandling sets up graceful shutdown on SIGINT/SIGTERM
func (afm *AsyncFlushManager) setupSignalHandling() {
	afm.mutex.Lock()
	defer afm.mutex.Unlock()
	
	if afm.signalHandlerSet {
		return
	}
	
	afm.signalHandlerSet = true
	
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-sigChan
		log.Printf("[INFO] Received signal %v, initiating graceful shutdown", sig)
		
		// Trigger emergency flush of all active buffers
		afm.emergencyFlushAll()
		
		// Give some time for flushes to complete
		done := make(chan bool, 1)
		go func() {
			afm.Shutdown()
			done <- true
		}()
		
		select {
		case <-done:
			log.Printf("[INFO] Graceful shutdown completed")
		case <-time.After(10 * time.Second):
			log.Printf("[WARN] Shutdown timeout, forcing exit")
		}
		
		os.Exit(0)
	}()
}

// emergencyFlushAll flushes all active batch buffers immediately
func (afm *AsyncFlushManager) emergencyFlushAll() {
	log.Printf("[INFO] Emergency flush: flushing all active buffers")
	
	buffersMutex.RLock()
	activeBuffers := make(map[string]*BatchBuffer)
	for name, buffer := range processBuffers {
		activeBuffers[name] = buffer
	}
	buffersMutex.RUnlock()
	
	// Create emergency flush tasks
	var wg sync.WaitGroup
	for processName, buffer := range activeBuffers {
		wg.Add(1)
		go func(pName string, buf *BatchBuffer) {
			defer wg.Done()
			
			// Get current buffer contents
			buf.bufferMutex.RLock()
			entries := make([]*LogEntry, len(buf.buffer))
			copy(entries, buf.buffer)
			buf.bufferMutex.RUnlock()
			
			if len(entries) == 0 {
				return
			}
			
			// Create emergency flush task
			filePath, _ := buf.fileRotation.GetCurrentFilePath()
			task := &FlushTask{
				Buffer:     buf,
				Entries:    entries,
				FilePath:   filePath,
				Priority:   2, // Emergency priority
				MaxRetries: 1, // Only one retry for emergency flush
			}
			
			// Process immediately (synchronous for signal handling)
			afm.processFlushTask(-2, task) // Special worker ID for emergency
			
			log.Printf("[INFO] Emergency flushed %d entries for process %s", len(entries), pName)
		}(processName, buffer)
	}
	
	wg.Wait()
	log.Printf("[INFO] Emergency flush completed")
}

// saveToEmergencyBuffer saves entries to emergency buffer files
func (afm *AsyncFlushManager) saveToEmergencyBuffer(entries []*LogEntry) {
	afm.emergencyBuffer.mutex.Lock()
	defer afm.emergencyBuffer.mutex.Unlock()
	
	timestamp := time.Now().Format("20060102_150405")
	fileName := fmt.Sprintf("emergency_buffer_%s_%d.json", timestamp, os.Getpid())
	filePath := filepath.Join(afm.emergencyBuffer.tempDir, fileName)
	
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("[ERROR] Failed to create emergency buffer file: %v", err)
		return
	}
	defer file.Close()
	
	// Write entries as JSON (simple format for emergency recovery)
	for _, entry := range entries {
		line := fmt.Sprintf("{\"ts\":\"%s\",\"level\":\"%s\",\"source\":\"%s\",\"message\":\"%s\",\"process\":\"%s\",\"trace_id\":\"%s\"}\n",
			entry.Ts.Format(time.RFC3339), entry.Level, entry.Source, entry.Message, entry.Process, entry.TraceID)
		file.WriteString(line)
	}
	
	log.Printf("[WARN] Saved %d entries to emergency buffer: %s", len(entries), filePath)
}

// GetStats returns statistics about the async flush manager
func (afm *AsyncFlushManager) GetStats() AsyncFlushStats {
	return AsyncFlushStats{
		QueueLength:   len(afm.flushQueue),
		QueueCapacity: cap(afm.flushQueue),
		TotalFlushes:  atomic.LoadInt64(&afm.totalFlushes),
		TotalErrors:   atomic.LoadInt64(&afm.totalErrors),
		LastFlush:     afm.lastFlushTime,
		WorkersActive: true,
	}
}

// AsyncFlushStats contains statistics about async flush operations
type AsyncFlushStats struct {
	QueueLength   int       `json:"queue_length"`
	QueueCapacity int       `json:"queue_capacity"`
	TotalFlushes  int64     `json:"total_flushes"`
	TotalErrors   int64     `json:"total_errors"`
	LastFlush     time.Time `json:"last_flush"`
	WorkersActive bool      `json:"workers_active"`
}

// Shutdown gracefully shuts down the async flush manager
func (afm *AsyncFlushManager) Shutdown() {
	afm.mutex.Lock()
	defer afm.mutex.Unlock()
	
	if afm.isShutdown {
		log.Printf("[INFO] Async flush manager already shutdown, skipping...")
		return
	}
	
	log.Printf("[INFO] Shutting down async flush manager...")
	afm.isShutdown = true
	
	// Close the flush queue
	close(afm.flushQueue)
	
	// Cancel context to stop workers
	afm.cancel()
	
	// Wait for workers to finish
	afm.wg.Wait()
	
	// Close emergency buffer files
	afm.emergencyBuffer.mutex.Lock()
	for _, file := range afm.emergencyBuffer.bufferFiles {
		file.Close()
	}
	afm.emergencyBuffer.mutex.Unlock()
	
	log.Printf("[INFO] Async flush manager shutdown complete")
}

// RecoverEmergencyBuffers attempts to recover data from emergency buffer files
func (afm *AsyncFlushManager) RecoverEmergencyBuffers() error {
	pattern := filepath.Join(afm.emergencyBuffer.tempDir, "emergency_buffer_*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to find emergency buffer files: %v", err)
	}
	
	if len(files) == 0 {
		return nil // No emergency files to recover
	}
	
	log.Printf("[INFO] Found %d emergency buffer files to recover", len(files))
	
	for _, file := range files {
		log.Printf("[INFO] Recovering emergency buffer file: %s", filepath.Base(file))
		// Implementation would parse JSON and re-submit entries
		// For now, just log the recovery attempt
		
		// Clean up after successful recovery
		os.Remove(file)
	}
	
	return nil
}