package db

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// UltraLightLogger provides ultra-lightweight logging with fixed memory usage
type UltraLightLogger struct {
	processName     string
	ringBuffer      *RingBuffer[*LogEntry]
	parquetStreamer *ParquetStreamer
	exportTicker    *time.Ticker
	exportInterval  time.Duration
	mutex           sync.RWMutex
	running         bool
	stopChan        chan struct{}
	wg              sync.WaitGroup

	// Configuration
	ultraFastMode bool
	autoExport    bool

	// Statistics
	totalEntries int64
	totalExports int64
	exportErrors int64
	lastExport   time.Time
}

// NewUltraLightLogger creates a new ultra-lightweight logger
func NewUltraLightLogger(processName string, bufferSize int, exportInterval time.Duration, ultraFastMode bool) *UltraLightLogger {
	baseDir := GetGlobalLakeDir()

	return &UltraLightLogger{
		processName:     processName,
		ringBuffer:      NewRingBuffer[*LogEntry](bufferSize),
		parquetStreamer: NewParquetStreamer(baseDir),
		exportInterval:  exportInterval,
		ultraFastMode:   ultraFastMode,
		autoExport:      true,
		stopChan:        make(chan struct{}),
		lastExport:      time.Now(),
	}
}

// Start starts the ultra-lightweight logger
func (ull *UltraLightLogger) Start() error {
	ull.mutex.Lock()
	defer ull.mutex.Unlock()

	if ull.running {
		return nil
	}

	ull.running = true

	if ull.autoExport {
		ull.exportTicker = time.NewTicker(ull.exportInterval)
		ull.wg.Add(1)
		go ull.backgroundExport()
	}

	mode := "Standard"
	if ull.ultraFastMode {
		mode = "Ultra-Fast"
	}

	log.Printf("[INFO] UltraLightLogger started for process %s (mode: %s, buffer: %d, export: %v)",
		ull.processName, mode, ull.ringBuffer.MaxSize(), ull.exportInterval)

	return nil
}

// Stop stops the ultra-lightweight logger
func (ull *UltraLightLogger) Stop() error {
	ull.mutex.Lock()
	if !ull.running {
		ull.mutex.Unlock()
		return nil
	}

	ull.running = false
	ull.mutex.Unlock()

	// Stop ticker
	if ull.exportTicker != nil {
		ull.exportTicker.Stop()
	}

	// Signal stop
	close(ull.stopChan)

	// Wait for background goroutines
	ull.wg.Wait()

	// Final export
	if err := ull.ExportToParquet(); err != nil {
		log.Printf("[ERROR] Final export failed: %v", err)
	}

	log.Printf("[INFO] UltraLightLogger stopped for process %s (total entries: %d, exports: %d)",
		ull.processName, ull.totalEntries, ull.totalExports)

	return nil
}

// AddEntry adds a log entry to the buffer
func (ull *UltraLightLogger) AddEntry(entry *LogEntry) error {
	ull.ringBuffer.Push(entry)
	ull.totalEntries++

	// Auto-export if buffer is full
	if ull.ringBuffer.IsFull() && ull.autoExport {
		go func() {
			if err := ull.ExportToParquet(); err != nil {
				log.Printf("[ERROR] Auto-export failed: %v", err)
				ull.exportErrors++
			}
		}()
	}

	return nil
}

// backgroundExport runs the periodic export process
func (ull *UltraLightLogger) backgroundExport() {
	defer ull.wg.Done()

	for {
		select {
		case <-ull.exportTicker.C:
			if err := ull.ExportToParquet(); err != nil {
				log.Printf("[ERROR] Background export failed: %v", err)
				ull.exportErrors++
			}
		case <-ull.stopChan:
			return
		}
	}
}

// ExportToParquet exports the current buffer to a parquet file
func (ull *UltraLightLogger) ExportToParquet() error {
	// Get all entries from ring buffer
	entries := ull.ringBuffer.ToSlice()
	if len(entries) == 0 {
		return nil
	}

	start := time.Now()

	var fileSize int64
	var err error

	// Choose export method based on mode
	if ull.ultraFastMode {
		_, fileSize, err = ull.parquetStreamer.StreamToParquetFast(entries, ull.processName)
	} else {
		_, fileSize, err = ull.parquetStreamer.StreamToParquet(entries, ull.processName)
	}

	if err != nil {
		return fmt.Errorf("failed to export to parquet: %v", err)
	}

	// Clear buffer after successful export
	ull.ringBuffer.Clear()

	ull.totalExports++
	ull.lastExport = time.Now()

	duration := time.Since(start)
	entriesPerMs := float64(len(entries)) / float64(duration.Milliseconds())

	log.Printf("[INFO] Exported %d entries in %v (%.2f entries/ms, size: %.2f KB)",
		len(entries), duration, entriesPerMs, float64(fileSize)/1024)

	return nil
}

// Search performs a columnar search across parquet files using DuckDB
func (ull *UltraLightLogger) Search(query ColumnarQuery) ([]*LogEntry, error) {
	// Set default limit if not specified
	if query.Limit <= 0 {
		query.Limit = 1000
	}

	// DuckDB will directly query parquet files
	// Implementation will be in query.go using DuckDB's read_parquet()
	return SearchParquetWithDuckDB(ull.processName, query)
}

// GetStats returns statistics about the ultra-lightweight logger
func (ull *UltraLightLogger) GetStats() UltraLightStats {
	bufferStats := ull.ringBuffer.Stats()

	return UltraLightStats{
		ProcessName:       ull.processName,
		BufferSize:        bufferStats.Size,
		BufferMaxSize:     bufferStats.MaxSize,
		BufferUtilization: bufferStats.Utilization,
		TotalEntries:      ull.totalEntries,
		TotalExports:      ull.totalExports,
		ExportErrors:      ull.exportErrors,
		LastExport:        ull.lastExport,
		TotalFiles:        0, // Will be calculated by DuckDB query if needed
		TotalStorageSize:  0, // Will be calculated by DuckDB query if needed
		UltraFastMode:     ull.ultraFastMode,
	}
}

// UltraLightStats contains statistics about the ultra-lightweight logger
type UltraLightStats struct {
	ProcessName       string    `json:"process_name"`
	BufferSize        int       `json:"buffer_size"`
	BufferMaxSize     int       `json:"buffer_max_size"`
	BufferUtilization float64   `json:"buffer_utilization"`
	TotalEntries      int64     `json:"total_entries"`
	TotalExports      int64     `json:"total_exports"`
	ExportErrors      int64     `json:"export_errors"`
	LastExport        time.Time `json:"last_export"`
	TotalFiles        int       `json:"total_files"`
	TotalStorageSize  int64     `json:"total_storage_size"`
	UltraFastMode     bool      `json:"ultra_fast_mode"`
}
