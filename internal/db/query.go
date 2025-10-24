package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"seyir/internal/config"
	"seyir/internal/logger"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// debugLog prints debug information only if query debug logging is enabled
func debugLog(format string, args ...interface{}) {
	if config.IsDebugEnabled() {
		logger.Debug(format, args...)
	}
}

// NewQueryFilter creates a new QueryFilter with required time range
// timeRange: "1h", "24h", "7d", "30d" - duration back from now
func NewQueryFilter(timeRange string) (*QueryFilter, error) {
	var duration time.Duration
	var err error
	
	switch timeRange {
	case "1h":
		duration = time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		duration, err = time.ParseDuration(timeRange)
		if err != nil {
			return nil, fmt.Errorf("invalid time range: %s (use formats like '1h', '24h', '7d', or Go duration format)", timeRange)
		}
	}
	
	now := time.Now()
	return &QueryFilter{
		StartTime: now.Add(-duration),
		EndTime:   now,
		Limit:     1000, // Default limit
	}, nil
}

// NewQueryFilterWithRange creates a QueryFilter with specific start and end times
func NewQueryFilterWithRange(startTime, endTime time.Time) *QueryFilter {
	return &QueryFilter{
		StartTime: startTime,
		EndTime:   endTime,
		Limit:     1000, // Default limit
	}
}

// ColumnarQuery represents a columnar search query
type ColumnarQuery struct {
	TraceID   string    `json:"trace_id"`
	Level     string    `json:"level"`
	Process   string    `json:"process"`
	Source    string    `json:"source"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Limit     int       `json:"limit"`
}

// QueryFilter represents search filters for efficient querying
// Focused on UI needs: ts, level, message, trace_id, source, tags
type QueryFilter struct {
	// Time range filters (REQUIRED - most efficient due to partition pruning)
	StartTime time.Time `json:"start_time"` // Required for all queries
	EndTime   time.Time `json:"end_time"`   // Required for all queries
	
	// Process filter (optional - enables folder-level optimization)
	ProcessName string `json:"process_name,omitempty"` // Search within specific process folder
	
	// Direct search filters
	MessageSearch string `json:"message_search,omitempty"` // Direct text search in message
	
	// Exact match filters (efficient with columnar storage)
	Sources  []string `json:"sources,omitempty"`  // Exact source matches
	Levels   []string `json:"levels,omitempty"`   // Exact level matches
	
	// ID-based filters (very efficient)
	TraceIDs []string `json:"trace_ids,omitempty"`
	
	// Tag filters (array operations)
	Tags []string `json:"tags,omitempty"` // Must contain all these tags
	
	// Result limits
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// QueryResult contains query results and metadata
type QueryResult struct {
	Entries      []*LogEntry
	TotalCount   int64
	QueryTime    time.Duration
	FilesScanned int
}



// GetDistinctValues returns distinct values for a specific column (useful for building filters)
func GetDistinctValues(column string, limit int) ([]string, error) {
	if !isValidColumn(column) {
		return nil, fmt.Errorf("invalid column name: %s", column)
	}
	
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set")
	}
	
	// Create temporary DuckDB connection
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %v", err)
	}
	defer db.Close()
	
	// Use glob pattern to read all parquet files in Hive partitioned structure
	parquetPattern := filepath.Join(lakeDir, "*", "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	
	// Check if any files exist
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return []string{}, nil
	}
	
	sql := fmt.Sprintf(`
		SELECT DISTINCT %s 
		FROM read_parquet('%s') 
		WHERE %s IS NOT NULL AND %s != '' 
		ORDER BY %s 
		LIMIT ?`, column, parquetPattern, column, column, column)
	
	rows, err := db.Query(sql, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get distinct values: %v", err)
	}
	defer rows.Close()
	
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			continue
		}
		values = append(values, value)
	}
	
	return values, nil
}

// isValidColumn checks if the column name is valid to prevent SQL injection
func isValidColumn(column string) bool {
	validColumns := map[string]bool{
		"source":     true,
		"level":      true,
		"process":    true,
		"component":  true,
		"thread":     true,
		"user_id":    true,
		"request_id": true,
		"trace_id":   true,
	}
	return validColumns[column]
}

// GetTimeRange returns the time range of all logs in the system
func GetTimeRange() (*time.Time, *time.Time, error) {
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, nil, fmt.Errorf("global lake directory not set")
	}
	
	// Create temporary DuckDB connection
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database: %v", err)
	}
	defer db.Close()
	
	// Use glob pattern to read all parquet files in Hive partitioned structure
	parquetPattern := filepath.Join(lakeDir, "*", "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	
	// Check if any files exist
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return nil, nil, nil
	}
	
	sql := fmt.Sprintf(`
		SELECT MIN(ts) as min_time, MAX(ts) as max_time 
		FROM read_parquet('%s')`, parquetPattern)
	
	var minTime, maxTime time.Time
	err = db.QueryRow(sql).Scan(&minTime, &maxTime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get time range: %v", err)
	}
	
	return &minTime, &maxTime, nil
}

// QueryStats returns statistics about the query performance and data
type QueryStats struct {
	TotalFiles      int           `json:"total_files"`
	TotalSizeBytes  int64         `json:"total_size_bytes"`
	OldestTimestamp *time.Time    `json:"oldest_timestamp"`
	NewestTimestamp *time.Time    `json:"newest_timestamp"`
	TotalRecords    int64         `json:"total_records"`
	UniqueProcesses int           `json:"unique_processes"`
	UniqueSources   int           `json:"unique_sources"`
	UniqueLevels    int           `json:"unique_levels"`
	QueryTime       time.Duration `json:"query_time"`
}

// GetQueryStats returns comprehensive statistics about the log data
func GetQueryStats() (*QueryStats, error) {
	start := time.Now()
	
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set")
	}
	
	// Get file statistics for Hive partitioned structure
	matches, err := filepath.Glob(filepath.Join(lakeDir, "*", "year=*", "month=*", "day=*", "hour=*", "*.parquet"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob files: %v", err)
	}
	
	var totalSize int64
	for _, file := range matches {
		if stat, err := os.Stat(file); err == nil {
			totalSize += stat.Size()
		}
	}
	
	if len(matches) == 0 {
		return &QueryStats{
			TotalFiles:   0,
			QueryTime:    time.Since(start),
		}, nil
	}
	
	// Create temporary DuckDB connection
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %v", err)
	}
	defer db.Close()
	
	parquetPattern := filepath.Join(lakeDir, "*", "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	
	// Get comprehensive statistics in a single query
	sql := fmt.Sprintf(`
		SELECT 
			COUNT(*) as total_records,
			MIN(ts) as min_time,
			MAX(ts) as max_time,
			COUNT(DISTINCT COALESCE(process, '')) as unique_processes,
			COUNT(DISTINCT source) as unique_sources,
			COUNT(DISTINCT level) as unique_levels
		FROM read_parquet('%s')`, parquetPattern)
	
	var totalRecords int64
	var minTime, maxTime time.Time
	var uniqueProcesses, uniqueSources, uniqueLevels int
	
	err = db.QueryRow(sql).Scan(&totalRecords, &minTime, &maxTime, 
		&uniqueProcesses, &uniqueSources, &uniqueLevels)
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %v", err)
	}
	
	return &QueryStats{
		TotalFiles:      len(matches),
		TotalSizeBytes:  totalSize,
		OldestTimestamp: &minTime,
		NewestTimestamp: &maxTime,
		TotalRecords:    totalRecords,
		UniqueProcesses: uniqueProcesses,
		UniqueSources:   uniqueSources,
		UniqueLevels:    uniqueLevels,
		QueryTime:       time.Since(start),
	}, nil
}

// Query performs a comprehensive search with folder structure awareness
// This is the single query method that should be used for all queries
func Query(filter *QueryFilter) (*QueryResult, error) {
	start := time.Now()
	
	// Validate required time range
	if filter.StartTime.IsZero() || filter.EndTime.IsZero() {
		return nil, fmt.Errorf("start_time and end_time are required for all queries")
	}
	
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set")
	}
	
	// Create temporary DuckDB connection
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create query database: %v", err)
	}
	defer db.Close()
	
	// Build optimized parquet pattern using time-based folder structure
	// Pattern: ~/.seyir/lake/[process]/year=YYYY/month=MM/day=DD/hour=HH/*.parquet
	parquetPattern := buildTimeAwareParquetPattern(lakeDir, filter.StartTime, filter.EndTime, filter.ProcessName)
	
	// Check if any files exist in the time range
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return &QueryResult{
			Entries:      []*LogEntry{},
			TotalCount:   0,
			QueryTime:    time.Since(start),
			FilesScanned: 0,
		}, nil
	}
	
	debugLog("DirectSearch scanning %d parquet files with pattern: %s", len(matches), parquetPattern)
	
	// Build efficient SQL query with full-text search capabilities
	baseSQL := fmt.Sprintf(`
		SELECT ts, level, message, 
		       COALESCE(trace_id, '') as trace_id,
		       source, 
		       tags,
		       id,
		       process
		FROM read_parquet('%s')`, parquetPattern)
	
	// Build WHERE clause with comprehensive filters
	whereConditions := []string{}
	args := []interface{}{}
	
	// Time range filters (enables partition pruning)
	whereConditions = append(whereConditions, "ts >= ?")
	args = append(args, filter.StartTime)
	whereConditions = append(whereConditions, "ts <= ?")
	args = append(args, filter.EndTime)
	
	// Direct message search with full-text capabilities
	if filter.MessageSearch != "" {
		// Use both LIKE and regex for comprehensive search
		whereConditions = append(whereConditions, "(message ILIKE ? OR message ~ ?)")
		searchPattern := "%" + filter.MessageSearch + "%"
		regexPattern := "(?i)" + filter.MessageSearch // Case-insensitive regex
		args = append(args, searchPattern, regexPattern)
	}
	
	// Exact match filters
	if len(filter.Sources) > 0 {
		placeholders := make([]string, len(filter.Sources))
		for i, source := range filter.Sources {
			placeholders[i] = "?"
			args = append(args, source)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("source IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	if len(filter.Levels) > 0 {
		placeholders := make([]string, len(filter.Levels))
		for i, level := range filter.Levels {
			placeholders[i] = "?"
			args = append(args, level)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("level IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	// ID-based filters
	if len(filter.TraceIDs) > 0 {
		placeholders := make([]string, len(filter.TraceIDs))
		for i, traceID := range filter.TraceIDs {
			placeholders[i] = "?"
			args = append(args, traceID)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("trace_id IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	// Tag filters
	for _, tag := range filter.Tags {
		whereConditions = append(whereConditions, "list_contains(string_split(tags, ','), ?)")
		args = append(args, tag)
	}
	
	// Build final query
	finalSQL := baseSQL + " WHERE " + strings.Join(whereConditions, " AND ")
	
	// Add deterministic ordering for consistent pagination
	finalSQL += " ORDER BY ts DESC, id ASC"
	if filter.Limit > 0 {
		finalSQL += " LIMIT ?"
		args = append(args, filter.Limit)
		
		if filter.Offset > 0 {
			finalSQL += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}
	
	debugLog("DirectSearch SQL: %s", finalSQL)
	debugLog("DirectSearch args: %v", args)
	
	// Execute query
	rows, err := db.Query(finalSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute direct search: %v", err)
	}
	defer rows.Close()
	
	// Scan results
	var entries []*LogEntry
	for rows.Next() {
		var e LogEntry
		var tagsStr sql.NullString
		var processStr sql.NullString
		
		err := rows.Scan(&e.Ts, &e.Level, &e.Message, &e.TraceID, &e.Source, &tagsStr, &e.ID, &processStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %v", err)
		}
		
		// Set process if available
		if processStr.Valid {
			e.Process = processStr.String
		}
		
		// Convert tags from comma-separated string back to array
		if tagsStr.Valid && tagsStr.String != "" {
			e.Tags = strings.Split(tagsStr.String, ",")
			for i := range e.Tags {
				e.Tags[i] = strings.TrimSpace(e.Tags[i])
			}
		}
		
		entries = append(entries, &e)
	}
	
	// Get total count for pagination
	totalCount, err := QueryCount(filter)
	if err != nil {
		logger.Warn("Failed to get total count: %v", err)
		totalCount = int64(len(entries))
	}
	
	return &QueryResult{
		Entries:      entries,
		TotalCount:   totalCount,
		QueryTime:    time.Since(start),
		FilesScanned: len(matches),
	}, nil
}

// QueryCount returns the total count for Query without LIMIT/OFFSET
func QueryCount(filter *QueryFilter) (int64, error) {
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return 0, fmt.Errorf("global lake directory not set")
	}
	
	// Create temporary DuckDB connection
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return 0, fmt.Errorf("failed to create count database: %v", err)
	}
	defer db.Close()
	
	// Build time-aware parquet pattern
	parquetPattern := buildTimeAwareParquetPattern(lakeDir, filter.StartTime, filter.EndTime, filter.ProcessName)
	
	// Check if any files exist
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return 0, nil
	}
	
	// Build count SQL (same WHERE conditions as DirectSearch)
	baseSQL := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet('%s')", parquetPattern)
	
	whereConditions := []string{}
	args := []interface{}{}
	
	// Time range filters
	whereConditions = append(whereConditions, "ts >= ?")
	args = append(args, filter.StartTime)
	whereConditions = append(whereConditions, "ts <= ?")
	args = append(args, filter.EndTime)
	
	// Message search
	if filter.MessageSearch != "" {
		whereConditions = append(whereConditions, "(message ILIKE ? OR message ~ ?)")
		searchPattern := "%" + filter.MessageSearch + "%"
		regexPattern := "(?i)" + filter.MessageSearch
		args = append(args, searchPattern, regexPattern)
	}
	
	// Other filters (same as DirectSearch)
	if len(filter.Sources) > 0 {
		placeholders := make([]string, len(filter.Sources))
		for i, source := range filter.Sources {
			placeholders[i] = "?"
			args = append(args, source)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("source IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	if len(filter.Levels) > 0 {
		placeholders := make([]string, len(filter.Levels))
		for i, level := range filter.Levels {
			placeholders[i] = "?"
			args = append(args, level)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("level IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	if len(filter.TraceIDs) > 0 {
		placeholders := make([]string, len(filter.TraceIDs))
		for i, traceID := range filter.TraceIDs {
			placeholders[i] = "?"
			args = append(args, traceID)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("trace_id IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	for _, tag := range filter.Tags {
		whereConditions = append(whereConditions, "list_contains(string_split(tags, ','), ?)")
		args = append(args, tag)
	}
	
	// Build final query
	finalSQL := baseSQL + " WHERE " + strings.Join(whereConditions, " AND ")
	
	var count int64
	err = db.QueryRow(finalSQL, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to execute count query: %v", err)
	}
	
	return count, nil
}

// buildTimeAwareParquetPattern creates an optimized parquet pattern based on time range
// If processName is provided, it searches only in that process's folder
func buildTimeAwareParquetPattern(lakeDir string, startTime, endTime time.Time, processName string) string {
	// For now, use a general pattern that covers all time ranges
	// TODO: Optimize this to build specific year/month/day/hour patterns based on time range
	
	processPattern := "*"
	if processName != "" {
		processPattern = processName
	}
	
	return filepath.Join(lakeDir, processPattern, "year=*", "month=*", "day=*", "hour=*", "*.parquet")
}





// GetProcessList returns a list of available processes in the lake directory
func GetProcessList() ([]string, error) {
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set")
	}
	
	// Read all process directories
	entries, err := os.ReadDir(lakeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read lake directory: %v", err)
	}
	
	var processes []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			processes = append(processes, entry.Name())
		}
	}
	
	return processes, nil
}

// GetFolderStructureInfo returns information about the folder structure for a process
type FolderStructureInfo struct {
	ProcessName     string                 `json:"process_name"`
	YearFolders     []string              `json:"year_folders"`
	TotalFiles      int                   `json:"total_files"`
	TotalSizeBytes  int64                 `json:"total_size_bytes"`
	DateRange       map[string]time.Time   `json:"date_range"`
	PartitionCounts map[string]int         `json:"partition_counts"`
}

// GetFolderStructureInfo returns detailed information about a process's folder structure
func GetFolderStructureInfo(processName string) (*FolderStructureInfo, error) {
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set")
	}
	
	processPath := filepath.Join(lakeDir, processName)
	if _, err := os.Stat(processPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("process '%s' not found", processName)
	}
	
	info := &FolderStructureInfo{
		ProcessName:     processName,
		DateRange:       make(map[string]time.Time),
		PartitionCounts: make(map[string]int),
	}
	
	// Walk through the Hive partition structure
	pattern := filepath.Join(processPath, "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to scan folder structure: %v", err)
	}
	
	yearSet := make(map[string]bool)
	var totalSize int64
	
	for _, file := range matches {
		// Extract year from path
		parts := strings.Split(file, string(filepath.Separator))
		for _, part := range parts {
			if strings.HasPrefix(part, "year=") {
				year := strings.TrimPrefix(part, "year=")
				yearSet[year] = true
				info.PartitionCounts[year]++
			}
		}
		
		// Get file size
		if stat, err := os.Stat(file); err == nil {
			totalSize += stat.Size()
		}
	}
	
	// Convert year set to slice
	for year := range yearSet {
		info.YearFolders = append(info.YearFolders, year)
	}
	
	info.TotalFiles = len(matches)
	info.TotalSizeBytes = totalSize
	
	// Get date range using DuckDB query
	if len(matches) > 0 {
		minTime, maxTime, err := getProcessTimeRange(processName)
		if err == nil {
			if minTime != nil {
				info.DateRange["oldest"] = *minTime
			}
			if maxTime != nil {
				info.DateRange["newest"] = *maxTime
			}
		}
	}
	
	return info, nil
}

// getProcessTimeRange returns the time range for a specific process
func getProcessTimeRange(processName string) (*time.Time, *time.Time, error) {
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return nil, nil, fmt.Errorf("global lake directory not set")
	}
	
	// Create temporary DuckDB connection
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database: %v", err)
	}
	defer db.Close()
	
	// Process-specific parquet pattern
	parquetPattern := filepath.Join(lakeDir, processName, "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	
	// Check if any files exist
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return nil, nil, nil
	}
	
	sql := fmt.Sprintf(`
		SELECT MIN(ts) as min_time, MAX(ts) as max_time 
		FROM read_parquet('%s')`, parquetPattern)
	
	var minTime, maxTime time.Time
	err = db.QueryRow(sql).Scan(&minTime, &maxTime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get time range: %v", err)
	}
	
	return &minTime, &maxTime, nil
}
