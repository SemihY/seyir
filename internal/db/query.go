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
	// Time range filters (most efficient due to partition pruning)
	StartTime *time.Time
	EndTime   *time.Time
	
	// Exact match filters (efficient with columnar storage)
	Sources  []string // Exact source matches
	Levels   []string // Exact level matches
	
	// ID-based filters (very efficient)
	TraceIDs []string
	
	// Tag filters (array operations)
	Tags []string // Must contain all these tags
	
	// Result limits
	Limit  int
	Offset int
}

// QueryResult contains query results and metadata
type QueryResult struct {
	Entries      []*LogEntry
	TotalCount   int64
	QueryTime    time.Duration
	FilesScanned int
}

// FastQuery performs efficient querying across all process logs using DuckDB's columnar capabilities
func FastQuery(filter *QueryFilter) (*QueryResult, error) {
	start := time.Now()
	
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
	
	// Use glob pattern to read all parquet files in Hive partitioned structure
	// This enables efficient partition pruning when time filters are used
	parquetPattern := filepath.Join(lakeDir, "*", "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	
	// Check if any files exist
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return &QueryResult{
			Entries:      []*LogEntry{},
			TotalCount:   0,
			QueryTime:    time.Since(start),
			FilesScanned: 0,
		}, nil
	}
	
	debugLog("FastQuery scanning %d parquet files with pattern: %s", len(matches), parquetPattern)
	
	// Build efficient SQL query with projection pushdown
	// Only select the columns needed by UI for maximum performance
	baseSQL := fmt.Sprintf(`
		SELECT ts, level, message, 
		       COALESCE(trace_id, '') as trace_id,
		       source, 
		       tags,
		       id
		FROM read_parquet('%s')`, parquetPattern)
	
	// Build WHERE clause with efficient filters
	whereConditions := []string{}
	args := []interface{}{}
	
	// Time range filters (enables partition pruning)
	if filter.StartTime != nil {
		whereConditions = append(whereConditions, "ts >= ?")
		args = append(args, *filter.StartTime)
	}
	if filter.EndTime != nil {
		whereConditions = append(whereConditions, "ts <= ?")
		args = append(args, *filter.EndTime)
	}
	
	// Exact match filters (very efficient with columnar storage)
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
	
	// ID-based filters (very efficient)
	if len(filter.TraceIDs) > 0 {
		placeholders := make([]string, len(filter.TraceIDs))
		for i, traceID := range filter.TraceIDs {
			placeholders[i] = "?"
			args = append(args, traceID)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("trace_id IN (%s)", strings.Join(placeholders, ", ")))
	}
	
	// Tag filters (array operations - less efficient but still better than LIKE)
	for _, tag := range filter.Tags {
		whereConditions = append(whereConditions, "list_contains(tags, ?)")
		args = append(args, tag)
	}
	
	// Build final query
	finalSQL := baseSQL
	if len(whereConditions) > 0 {
		finalSQL += " WHERE " + strings.Join(whereConditions, " AND ")
	}
	
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
	
	debugLog("FastQuery SQL: %s", finalSQL)
	debugLog("FastQuery args: %v", args)
	
	// Execute query
	rows, err := db.Query(finalSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute fast query: %v", err)
	}
	defer rows.Close()
	
	// Scan results - only the projected columns
	var entries []*LogEntry
	for rows.Next() {
		var e LogEntry
		var tagsStr sql.NullString
		
		// Scan only the projected columns for maximum performance
		err := rows.Scan(&e.Ts, &e.Level, &e.Message, &e.TraceID, &e.Source, &tagsStr, &e.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %v", err)
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
	
	// Get total count (without LIMIT) for pagination
	totalCount, err := FastCount(filter)
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

// FastCount returns the total count of matching records (without LIMIT/OFFSET)
func FastCount(filter *QueryFilter) (int64, error) {
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
	
	// Use glob pattern to read all parquet files in Hive partitioned structure
	parquetPattern := filepath.Join(lakeDir, "*", "year=*", "month=*", "day=*", "hour=*", "*.parquet")
	
	// Check if any files exist
	matches, err := filepath.Glob(parquetPattern)
	if err != nil || len(matches) == 0 {
		return 0, nil
	}
	
	// Build count SQL (same WHERE conditions as FastQuery)
	baseSQL := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet('%s')", parquetPattern)
	
	whereConditions := []string{}
	args := []interface{}{}
	
	// Time range filters
	if filter.StartTime != nil {
		whereConditions = append(whereConditions, "ts >= ?")
		args = append(args, *filter.StartTime)
	}
	if filter.EndTime != nil {
		whereConditions = append(whereConditions, "ts <= ?")
		args = append(args, *filter.EndTime)
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
		whereConditions = append(whereConditions, "list_contains(tags, ?)")
		args = append(args, tag)
	}
	
	// Build final query
	finalSQL := baseSQL
	if len(whereConditions) > 0 {
		finalSQL += " WHERE " + strings.Join(whereConditions, " AND ")
	}
	
	var count int64
	err = db.QueryRow(finalSQL, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to execute count query: %v", err)
	}
	
	return count, nil
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

// SearchParquetWithDuckDB performs a search on parquet files using DuckDB
// This replaces the metadata index approach with direct DuckDB queries
func SearchParquetWithDuckDB(processName string, query ColumnarQuery) ([]*LogEntry, error) {
	start := time.Now()
	baseDir := GetGlobalLakeDir()
	
	// Build the parquet file pattern
	// Pattern: ~/.seyir/lake/processName/**/*.parquet
	parquetPattern := filepath.Join(baseDir, processName, "**", "*.parquet")
	
	// Create a temporary DuckDB connection for the query
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB: %v", err)
	}
	defer db.Close()
	
	// Build WHERE clause
	var whereClauses []string
	var args []interface{}
	argIdx := 1
	
	if query.TraceID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("trace_id = $%d", argIdx))
		args = append(args, query.TraceID)
		argIdx++
	}
	
	if query.Level != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("level = $%d", argIdx))
		args = append(args, query.Level)
		argIdx++
	}
	
	if query.Source != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("source = $%d", argIdx))
		args = append(args, query.Source)
		argIdx++
	}
	
	if !query.StartTime.IsZero() {
		whereClauses = append(whereClauses, fmt.Sprintf("ts >= $%d", argIdx))
		args = append(args, query.StartTime)
		argIdx++
	}
	
	if !query.EndTime.IsZero() {
		whereClauses = append(whereClauses, fmt.Sprintf("ts <= $%d", argIdx))
		args = append(args, query.EndTime)
		argIdx++
	}
	
	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}
	
	// Set default limit
	limit := query.Limit
	if limit <= 0 {
		limit = 1000
	}
	
	// Build DuckDB query using read_parquet with glob pattern
	sqlQuery := fmt.Sprintf(`
		SELECT 
			ts, source, level, message, id, trace_id, 
			process, component, thread, user_id, request_id, tags
		FROM read_parquet('%s', union_by_name=true, filename=true)
		%s
		ORDER BY ts DESC
		LIMIT %d
	`, parquetPattern, whereClause, limit)
	
	debugLog("Executing DuckDB query: %s", sqlQuery)
	debugLog("Query args: %v", args)
	
	// Execute query
	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("DuckDB query failed: %v", err)
	}
	defer rows.Close()
	
	// Parse results
	var results []*LogEntry
	for rows.Next() {
		entry := &LogEntry{}
		var tagsStr sql.NullString
		
		err := rows.Scan(
			&entry.Ts,
			&entry.Source,
			&entry.Level,
			&entry.Message,
			&entry.ID,
			&entry.TraceID,
			&entry.Process,
			&entry.Component,
			&entry.Thread,
			&entry.UserID,
			&entry.RequestID,
			&tagsStr,
		)
		
		if err != nil {
			logger.Warn("Failed to scan row: %v", err)
			continue
		}
		
		// Parse tags if present
		if tagsStr.Valid && tagsStr.String != "" {
			// DuckDB array format: ['tag1', 'tag2']
			tagsStr.String = strings.Trim(tagsStr.String, "[]")
			if tagsStr.String != "" {
				entry.Tags = strings.Split(tagsStr.String, ",")
				for i := range entry.Tags {
					entry.Tags[i] = strings.Trim(entry.Tags[i], " '\"")
				}
			}
		}
		
		results = append(results, entry)
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %v", err)
	}
	
	duration := time.Since(start)
	logger.Info("DuckDB search completed: %d results in %v", len(results), duration)
	
	return results, nil
}
