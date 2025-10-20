package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// CompactionManager manages the background compaction of small parquet files
type CompactionManager struct {
	lakeDir           string
	config            *CompactionConfig
	running           bool
	stopChan          chan struct{}
	wg                sync.WaitGroup
	mutex             sync.Mutex
	
	// Statistics
	totalCompactions  int64
	totalFilesMerged  int64
	totalBytesReclaimed int64
	lastCompaction    time.Time
}

// CompactionConfig holds configuration for file compaction
type CompactionConfig struct {
	Enabled              bool          `json:"enabled"`
	IntervalHours        int           `json:"interval_hours"`
	MinFilesForCompaction int          `json:"min_files_for_compaction"`
	MaxFileAge           time.Duration `json:"max_file_age"`
	MaxCompactedSizeMB   int64         `json:"max_compacted_size_mb"`
	ConcurrentPartitions int           `json:"concurrent_partitions"`
	DeleteOriginals      bool          `json:"delete_originals"`
}

// DefaultCompactionConfig returns sensible defaults
func DefaultCompactionConfig() *CompactionConfig {
	return &CompactionConfig{
		Enabled:              true,
		IntervalHours:        6, // Run every 6 hours
		MinFilesForCompaction: 5, // At least 5 small files to trigger compaction
		MaxFileAge:           2 * time.Hour, // Only compact files older than 2 hours
		MaxCompactedSizeMB:   100, // Don't create files larger than 100MB
		ConcurrentPartitions: 2, // Process 2 partitions concurrently
		DeleteOriginals:      true, // Delete original files after successful compaction
	}
}

// PartitionInfo represents a Hive partition with files to compact
type PartitionInfo struct {
	Path        string
	Process     string
	Year        int
	Month       int
	Day         int
	Hour        int
	Files       []FileInfo
	TotalSize   int64
	NeedsCompaction bool
}

// FileInfo represents a parquet file with metadata
type FileInfo struct {
	Path         string
	Size         int64
	ModTime      time.Time
	IsCompacted  bool // Whether this file was created by compaction
}

var (
	globalCompactionManager *CompactionManager
	compactionManagerOnce   sync.Once
)

// GetCompactionManager returns the global compaction manager
func GetCompactionManager() *CompactionManager {
	compactionManagerOnce.Do(func() {
		lakeDir := GetGlobalLakeDir()
		config := DefaultCompactionConfig()
		globalCompactionManager = NewCompactionManager(lakeDir, config)
	})
	return globalCompactionManager
}

// NewCompactionManager creates a new compaction manager
func NewCompactionManager(lakeDir string, config *CompactionConfig) *CompactionManager {
	if config == nil {
		config = DefaultCompactionConfig()
	}
	
	cm := &CompactionManager{
		lakeDir:        lakeDir,
		config:         config,
		stopChan:       make(chan struct{}),
		lastCompaction: time.Now(),
	}
	
	return cm
}

// Start starts the background compaction process
func (cm *CompactionManager) Start() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	if cm.running {
		return fmt.Errorf("compaction manager already running")
	}
	
	if !cm.config.Enabled {
		log.Printf("[INFO] Compaction is disabled, skipping")
		return nil
	}
	
	cm.running = true
	cm.wg.Add(1)
	
	go cm.compactionWorker()
	
	log.Printf("[INFO] Compaction manager started (interval: %dh, min files: %d)", 
		cm.config.IntervalHours, cm.config.MinFilesForCompaction)
	
	return nil
}

// Stop stops the background compaction process
func (cm *CompactionManager) Stop() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	if !cm.running {
		return
	}
	
	log.Printf("[INFO] Stopping compaction manager...")
	close(cm.stopChan)
	cm.running = false
	cm.wg.Wait()
	
	log.Printf("[INFO] Compaction manager stopped")
}

// compactionWorker runs the background compaction loop
func (cm *CompactionManager) compactionWorker() {
	defer cm.wg.Done()
	
	ticker := time.NewTicker(time.Duration(cm.config.IntervalHours) * time.Hour)
	defer ticker.Stop()
	
	// Run initial compaction after 1 minute
	initialTimer := time.NewTimer(1 * time.Minute)
	defer initialTimer.Stop()
	
	for {
		select {
		case <-initialTimer.C:
			cm.runCompaction()
			
		case <-ticker.C:
			cm.runCompaction()
			
		case <-cm.stopChan:
			return
		}
	}
}

// runCompaction performs a full compaction cycle
func (cm *CompactionManager) runCompaction() {
	start := time.Now()
	log.Printf("[INFO] Starting compaction cycle...")
	
	// Discover partitions that need compaction
	partitions, err := cm.discoverPartitions()
	if err != nil {
		log.Printf("[ERROR] Failed to discover partitions for compaction: %v", err)
		return
	}
	
	if len(partitions) == 0 {
		log.Printf("[INFO] No partitions need compaction")
		return
	}
	
	log.Printf("[INFO] Found %d partitions that need compaction", len(partitions))
	
	// Process partitions concurrently
	semaphore := make(chan struct{}, cm.config.ConcurrentPartitions)
	var wg sync.WaitGroup
	
	compactedCount := 0
	
	for _, partition := range partitions {
		if !partition.NeedsCompaction {
			continue
		}
		
		wg.Add(1)
		go func(p PartitionInfo) {
			defer wg.Done()
			
			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			
			if err := cm.compactPartition(&p); err != nil {
				log.Printf("[ERROR] Failed to compact partition %s: %v", p.Path, err)
			} else {
				log.Printf("[INFO] Successfully compacted partition %s (%d files -> 1 file)", 
					p.Path, len(p.Files))
				compactedCount++
			}
		}(*partition)
	}
	
	wg.Wait()
	
	cm.lastCompaction = time.Now()
	log.Printf("[INFO] Compaction cycle completed in %v (compacted %d partitions)", 
		time.Since(start), compactedCount)
}

// discoverPartitions scans the lake directory for partitions that need compaction
func (cm *CompactionManager) discoverPartitions() ([]*PartitionInfo, error) {
	var partitions []*PartitionInfo
	
	// Walk through the lake directory structure
	err := filepath.Walk(cm.lakeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip if not a directory or doesn't match Hive partition pattern
		if !info.IsDir() {
			return nil
		}
		
		// Check if this is an hour partition (deepest level)
		if strings.Contains(filepath.Base(path), "hour=") {
			partition, err := cm.analyzePartition(path)
			if err != nil {
				log.Printf("[WARN] Failed to analyze partition %s: %v", path, err)
				return nil
			}
			
			if partition != nil {
				partitions = append(partitions, partition)
			}
		}
		
		return nil
	})
	
	return partitions, err
}

// analyzePartition analyzes a partition directory to determine if compaction is needed
func (cm *CompactionManager) analyzePartition(partitionPath string) (*PartitionInfo, error) {
	// Parse partition path to extract metadata
	// Expected format: .../process_name/year=2024/month=10/day=07/hour=15/
	pathParts := strings.Split(partitionPath, string(filepath.Separator))
	if len(pathParts) < 4 {
		return nil, nil // Not a valid partition path
	}
	
	var process, year, month, day, hour string
	for i := len(pathParts) - 4; i < len(pathParts); i++ {
		part := pathParts[i]
		if strings.HasPrefix(part, "year=") {
			year = strings.TrimPrefix(part, "year=")
		} else if strings.HasPrefix(part, "month=") {
			month = strings.TrimPrefix(part, "month=")
		} else if strings.HasPrefix(part, "day=") {
			day = strings.TrimPrefix(part, "day=")
		} else if strings.HasPrefix(part, "hour=") {
			hour = strings.TrimPrefix(part, "hour=")
		} else if process == "" && !strings.Contains(part, "=") {
			process = part
		}
	}
	
	if process == "" || year == "" || month == "" || day == "" || hour == "" {
		return nil, nil // Invalid partition structure
	}
	
	// List all parquet files in the partition
	files, err := filepath.Glob(filepath.Join(partitionPath, "*.parquet"))
	if err != nil {
		return nil, err
	}
	
	if len(files) < cm.config.MinFilesForCompaction {
		return nil, nil // Not enough files to warrant compaction
	}
	
	var fileInfos []FileInfo
	var totalSize int64
	oldFileCount := 0
	
	for _, file := range files {
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		
		// Check if file is old enough for compaction
		if time.Since(stat.ModTime()) < cm.config.MaxFileAge {
			continue // Skip recent files
		}
		
		isCompacted := strings.Contains(filepath.Base(file), "compacted_")
		if !isCompacted {
			oldFileCount++
		}
		
		fileInfos = append(fileInfos, FileInfo{
			Path:        file,
			Size:        stat.Size(),
			ModTime:     stat.ModTime(),
			IsCompacted: isCompacted,
		})
		
		totalSize += stat.Size()
	}
	
	// Only compact if we have enough non-compacted files
	needsCompaction := oldFileCount >= cm.config.MinFilesForCompaction
	
	partition := &PartitionInfo{
		Path:            partitionPath,
		Process:         process,
		Files:           fileInfos,
		TotalSize:       totalSize,
		NeedsCompaction: needsCompaction,
	}
	
	return partition, nil
}

// compactPartition compacts all eligible files in a partition into a single file
func (cm *CompactionManager) compactPartition(partition *PartitionInfo) error {
	if len(partition.Files) == 0 {
		return nil
	}
	
	start := time.Now()
	
	// Create temporary DuckDB connection for compaction
	tempDB, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("failed to create temp database: %v", err)
	}
	defer tempDB.Close()
	
	// Create a union of all files in the partition
	var filePatterns []string
	var filesToDelete []string
	
	for _, fileInfo := range partition.Files {
		// Skip already compacted files if they're recent
		if fileInfo.IsCompacted && time.Since(fileInfo.ModTime) < 24*time.Hour {
			continue
		}
		
		filePatterns = append(filePatterns, fmt.Sprintf("'%s'", fileInfo.Path))
		filesToDelete = append(filesToDelete, fileInfo.Path)
	}
	
	if len(filePatterns) < 2 {
		return nil // Not enough files to compact
	}
	
	// Create compacted filename
	timestamp := time.Now().Format("20060102_150405")
	compactedFile := filepath.Join(partition.Path, fmt.Sprintf("compacted_%s.parquet", timestamp))
	
	// Read all files and write to compacted file
	unionPattern := strings.Join(filePatterns, ", ")
	
	log.Printf("[DEBUG] Compacting %d files in partition %s", len(filePatterns), partition.Path)
	
	// Use DuckDB's efficient UNION ALL to combine files
	sql := fmt.Sprintf(`
		COPY (
			SELECT * FROM read_parquet([%s])
			ORDER BY ts
		) TO '%s' (FORMAT PARQUET, COMPRESSION 'ZSTD', ROW_GROUP_SIZE 131072, COMPRESSION_LEVEL 3)
	`, unionPattern, compactedFile)
	
	_, err = tempDB.Exec(sql)
	if err != nil {
		return fmt.Errorf("failed to compact files: %v", err)
	}
	
	// Verify the compacted file was created successfully
	if stat, err := os.Stat(compactedFile); err != nil || stat.Size() == 0 {
		return fmt.Errorf("compacted file was not created properly")
	}
	
	// Delete original files if configured to do so
	if cm.config.DeleteOriginals {
		var deleteErrors []error
		for _, file := range filesToDelete {
			if err := os.Remove(file); err != nil {
				deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete %s: %v", file, err))
			}
		}
		
		if len(deleteErrors) > 0 {
			log.Printf("[WARN] Some files could not be deleted after compaction: %v", deleteErrors)
		}
	}
	
	// Update statistics
	cm.totalCompactions++
	cm.totalFilesMerged += int64(len(filesToDelete))
	
	log.Printf("[DEBUG] Compacted %d files into %s in %v", 
		len(filesToDelete), filepath.Base(compactedFile), time.Since(start))
	
	return nil
}

// ForceCompaction manually triggers a compaction cycle
func (cm *CompactionManager) ForceCompaction() error {
	if !cm.running {
		return fmt.Errorf("compaction manager is not running")
	}
	
	log.Printf("[INFO] Forcing compaction cycle...")
	go cm.runCompaction()
	
	return nil
}

// GetStats returns compaction statistics
func (cm *CompactionManager) GetStats() CompactionStats {
	return CompactionStats{
		TotalCompactions:  cm.totalCompactions,
		TotalFilesMerged:  cm.totalFilesMerged,
		LastCompaction:    cm.lastCompaction,
		IsRunning:         cm.running,
		Config:           *cm.config,
	}
}

// CompactionStats contains statistics about compaction operations
type CompactionStats struct {
	TotalCompactions  int64           `json:"total_compactions"`
	TotalFilesMerged  int64           `json:"total_files_merged"`
	LastCompaction    time.Time       `json:"last_compaction"`
	IsRunning         bool            `json:"is_running"`
	Config            CompactionConfig `json:"config"`
}