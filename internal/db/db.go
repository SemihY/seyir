package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

var (
	// Global lake directory where all session DBs are stored
	globalLakeDir string
	lakeDirMutex  sync.RWMutex
	
	// Active sessions tracking
	activeSessions map[string]*DB
	sessionsMutex  sync.RWMutex
)

// DB wraps sql.DB to provide a type in the core package
type DB struct {
	*sql.DB
	sessionID      string    // Session ID for this pipe operation
	parquetPath    string    // Path to the Parquet file for this session
	createdAt      time.Time // When this session was created
	lastActivity   time.Time // Last time this session was used
	processInfo    string    // Information about the process using this session
	pendingEntries int       // Number of entries waiting to be flushed
	lastFlush      time.Time // Last time data was flushed to Parquet
}

// SetGlobalLakeDir sets the global lake directory where all session DBs are stored
func SetGlobalLakeDir(dir string) {
	lakeDirMutex.Lock()
	defer lakeDirMutex.Unlock()
	globalLakeDir = dir
	
	// Initialize session tracking if not already done
	if activeSessions == nil {
		activeSessions = make(map[string]*DB)
	}
}

// GetGlobalLakeDir returns the global lake directory
func GetGlobalLakeDir() string {
	lakeDirMutex.RLock()
	defer lakeDirMutex.RUnlock()
	return globalLakeDir
}

// generateSessionID creates a unique session ID for this pipe operation
func generateSessionID() string {
	return fmt.Sprintf("session_%d_%d_%d", time.Now().Unix(), os.Getpid(), time.Now().UnixNano()%1000000)
}

// NewSessionConnection creates a new DuckDB session that writes to Parquet files
// Each pipe operation writes to its own Parquet file in the lake directory
func NewSessionConnection() (*DB, error) {
	lakeDirMutex.RLock()
	lakeDir := globalLakeDir
	lakeDirMutex.RUnlock()
	
	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set. Call SetGlobalLakeDir() first")
	}
	
	// Ensure the lake directory exists
	if err := os.MkdirAll(lakeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lake directory %s: %v", lakeDir, err)
	}

	// Generate unique session ID for this pipe operation
	sessionID := generateSessionID()
	
	// Get process information for logging
	processInfo := fmt.Sprintf("PID:%d", os.Getpid())
	if cmd := os.Getenv("_"); cmd != "" {
		processInfo += fmt.Sprintf(" CMD:%s", filepath.Base(cmd))
	}
	
	log.Printf("[INFO] Creating new session: %s (Process: %s)", sessionID, processInfo)

	// Create in-memory DuckDB instance for this session
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil { 
		return nil, fmt.Errorf("failed to create in-memory database: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	// Initialize database wrapper with session info
	now := time.Now()
	dbWrapper := &DB{
		DB:             db,
		sessionID:      sessionID,
		parquetPath:    filepath.Join(lakeDir, fmt.Sprintf("session_%s.parquet", sessionID)),
		createdAt:      now,
		lastActivity:   now,
		processInfo:    processInfo,
		pendingEntries: 0,
		lastFlush:      now,
	}

	// Create temporary table for this session
	_, err = dbWrapper.Exec(`
		CREATE TABLE logs (
			ts TIMESTAMP NOT NULL,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			id TEXT NOT NULL
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create temporary logs table: %v", err)
	}

	// Register this session in the global tracker
	sessionsMutex.Lock()
	activeSessions[sessionID] = dbWrapper
	sessionsMutex.Unlock()
	
	log.Printf("[INFO] Session %s registered. Active sessions: %d", sessionID, len(activeSessions))

	return dbWrapper, nil
}

func InitDB(path string) *DB {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("Failed to create directory %s: %v", dir, err)
	}

	// Open/create DuckDB database
	db, err := sql.Open("duckdb", path)
	if err != nil { 
		log.Fatalf("Failed to open/create database %s: %v", path, err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Initialize database wrapper
	dbWrapper := &DB{DB: db}

	// Ensure proper database schema
	if err := dbWrapper.EnsureSchema(); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	return dbWrapper
}

// EnsureSchema creates the necessary tables and indexes if they don't exist
func (db *DB) EnsureSchema() error {
	// For Parquet-based sessions, the table is already created in NewSessionConnection
	// This method is kept for compatibility with InitDB
	if db.sessionID != "" {
		// This is a session-based connection, table already created
		return nil
	}
	
	// For regular InitDB connections, create the logs table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS logs (
			ts TIMESTAMP NOT NULL,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			id TEXT NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	return nil
}

// HealthCheck verifies the database connection and basic functionality
func (db *DB) HealthCheck() error {
	// Test basic connectivity
	if err := db.Ping(); err != nil {
		return err
	}

	// Test basic query functionality
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
	if err != nil {
		return err
	}

	return nil
}

func SaveLog(db *DB, e *LogEntry) {
	// Insert into in-memory table
	_, err := db.Exec(`INSERT INTO logs VALUES (?, ?, ?, ?, ?)`, e.Ts, e.Source, e.Level, e.Message, e.ID)
	if err != nil { 
		log.Printf("[ERROR] failed to save log to memory: %v", err) 
	}
}

// SaveLogDirectly saves a log entry, batching writes for efficiency
func (db *DB) SaveLogDirectly(e *LogEntry) error {
	if db.sessionID == "" {
		return fmt.Errorf("no session ID set for this database")
	}
	
	// Update last activity time
	db.lastActivity = time.Now()
	
	// Insert into in-memory table first
	_, err := db.Exec(`INSERT INTO logs VALUES (?, ?, ?, ?, ?)`, e.Ts, e.Source, e.Level, e.Message, e.ID)
	if err != nil {
		return fmt.Errorf("failed to insert into memory table: %v", err)
	}
	
	db.pendingEntries++
	
	// Flush conditions: 
	// Since DuckDB APPEND mode has issues, we use larger batches and final flush
	// 1. Every 50 entries for reasonable memory usage
	// 2. Every 10 seconds to prevent data loss for long-running processes
	shouldFlush := db.pendingEntries >= 50 || 
		time.Since(db.lastFlush) >= 10*time.Second
	
	if shouldFlush {
		return db.flushToParquet()
	}
	
	return nil
}

// flushToParquet writes all pending entries to the Parquet file
func (db *DB) flushToParquet() error {
	if db.pendingEntries == 0 {
		return nil // Nothing to flush
	}
	
	// Check how many entries we're about to flush
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count logs before flush: %v", err)
	}
	
	if count == 0 {
		db.pendingEntries = 0
		return nil // Nothing to flush
	}
	
	// Check if the Parquet file exists
	fileExists := false
	if _, err := os.Stat(db.parquetPath); err == nil {
		fileExists = true
	}
	
	// Export to compressed Parquet
	var exportSQL string
	if fileExists {
		// Since APPEND mode has issues, implement manual append by reading existing data
		// First, read existing data into a temporary table
		_, err = db.Exec("CREATE TEMPORARY TABLE existing_logs AS SELECT * FROM read_parquet(?)", db.parquetPath)
		if err != nil {
			return fmt.Errorf("failed to read existing parquet data: %v", err)
		}
		
		// Insert existing data into our logs table (before new data)
		_, err = db.Exec("INSERT INTO logs SELECT * FROM existing_logs")
		if err != nil {
			return fmt.Errorf("failed to merge existing data: %v", err)
		}
		
		// Drop temporary table
		db.Exec("DROP TABLE existing_logs")
		
		// Update count to reflect total entries
		err = db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to count merged logs: %v", err)
		}
		
		log.Printf("[DEBUG] Merged with existing data, total entries: %d", count)
	}
	
	// Write all data (existing + new) with compression
	exportSQL = fmt.Sprintf("COPY logs TO '%s' (FORMAT PARQUET, COMPRESSION 'SNAPPY')", db.parquetPath)
	
	log.Printf("[DEBUG] Flushing %d entries to %s (file exists: %t)", count, filepath.Base(db.parquetPath), fileExists)
	
	_, err = db.Exec(exportSQL)
	if err != nil {
		return fmt.Errorf("failed to write to parquet file %s: %v", db.parquetPath, err)
	}
	
	// Clear the in-memory table after successful write
	_, err = db.Exec("DELETE FROM logs")
	if err != nil {
		log.Printf("[WARN] Failed to clear memory table: %v", err)
	}
	
	// Update flush tracking
	db.pendingEntries = 0
	db.lastFlush = time.Now()
	
	log.Printf("[DEBUG] Successfully flushed %d entries to %s", count, filepath.Base(db.parquetPath))
	return nil
}

// FlushToParquet exports all logs from the in-memory table to a Parquet file
func (db *DB) FlushToParquet() error {
	if db.parquetPath == "" {
		return fmt.Errorf("no parquet path set for this session")
	}
	
	// Check if there are any rows to export
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count logs: %v", err)
	}
	
	if count == 0 {
		return nil // Nothing to flush
	}
	
	// Export the in-memory logs table to Parquet (append mode)
	exportSQL := fmt.Sprintf("COPY logs TO '%s' (FORMAT PARQUET, APPEND)", db.parquetPath)
	_, err = db.Exec(exportSQL)
	if err != nil {
		return fmt.Errorf("failed to export logs to parquet file %s: %v", db.parquetPath, err)
	}
	
	return nil
}

// ClearMemoryTable removes all entries from the in-memory logs table
func (db *DB) ClearMemoryTable() error {
	_, err := db.Exec("DELETE FROM logs")
	if err != nil {
		return fmt.Errorf("failed to clear memory table: %v", err)
	}
	return nil
}

func SearchLogs(query string, limit int) ([]*LogEntry, error) {
	lakeDirMutex.RLock()
	lakeDir := globalLakeDir
	lakeDirMutex.RUnlock()

	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set. Call SetGlobalLakeDir() first")
	}

	// Create a temporary DuckDB connection to query Parquet files
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create in-memory database: %v", err)
	}
	defer db.Close()

	// Find all session Parquet files in the lake directory
	parquetPattern := filepath.Join(lakeDir, "session_*.parquet")
	
	// Create a query that reads from all Parquet files using glob pattern
	baseSQL := fmt.Sprintf(`
		SELECT ts, source, level, message, id 
		FROM read_parquet('%s')`, parquetPattern)
	
	log.Printf("[INFO] Searching Parquet files with pattern: %s", parquetPattern)

	var rows *sql.Rows
	if query == "*" {
		// Show all logs
		fullSQL := baseSQL + ` ORDER BY ts DESC LIMIT ?`
		rows, err = db.Query(fullSQL, limit)
	} else {
		// Search with filter using parameterized queries
		fullSQL := baseSQL + ` WHERE message LIKE ? OR source LIKE ? ORDER BY ts DESC LIMIT ?`
		queryPattern := "%" + query + "%"
		rows, err = db.Query(fullSQL, queryPattern, queryPattern, limit)
	}
	if err != nil {
		// Check if no parquet files exist yet
		matches, globErr := filepath.Glob(parquetPattern)
		if globErr == nil && len(matches) == 0 {
			log.Printf("[INFO] No Parquet files found in lake directory")
			return []*LogEntry{}, nil
		}
		return nil, fmt.Errorf("failed to query parquet files: %v", err)
	}
	defer rows.Close()

	var results []*LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.Ts, &e.Source, &e.Level, &e.Message, &e.ID); err != nil {
			return nil, err
		}
		results = append(results, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// CountLogs returns the total number of logs matching the query
func CountLogs(query string) (int, error) {
	lakeDirMutex.RLock()
	lakeDir := globalLakeDir
	lakeDirMutex.RUnlock()

	if lakeDir == "" {
		return 0, fmt.Errorf("global lake directory not set. Call SetGlobalLakeDir() first")
	}

	// Create a temporary DuckDB connection to query Parquet files
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return 0, fmt.Errorf("failed to create in-memory database: %v", err)
	}
	defer db.Close()

	// Find all session Parquet files in the lake directory
	parquetPattern := filepath.Join(lakeDir, "session_*.parquet")
	
	// Create a query that counts rows from all Parquet files using glob pattern
	baseSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM read_parquet('%s')`, parquetPattern)
	
	var row *sql.Row
	if query == "*" {
		// Count all logs
		row = db.QueryRow(baseSQL)
	} else {
		// Count with filter
		fullSQL := baseSQL + ` WHERE message LIKE ? OR source LIKE ?`
		queryPattern := "%" + query + "%"
		row = db.QueryRow(fullSQL, queryPattern, queryPattern)
	}
	
	var count int
	if err := row.Scan(&count); err != nil {
		// Check if no parquet files exist yet
		matches, globErr := filepath.Glob(parquetPattern)
		if globErr == nil && len(matches) == 0 {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to count logs: %v", err)
	}
	
	return count, nil
}

// SearchLogsWithPagination returns logs with pagination support
func SearchLogsWithPagination(query string, limit int, offset int) ([]*LogEntry, error) {
	lakeDirMutex.RLock()
	lakeDir := globalLakeDir
	lakeDirMutex.RUnlock()

	if lakeDir == "" {
		return nil, fmt.Errorf("global lake directory not set. Call SetGlobalLakeDir() first")
	}

	// Create a temporary DuckDB connection to query Parquet files
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create in-memory database: %v", err)
	}
	defer db.Close()

	// Find all session Parquet files in the lake directory
	parquetPattern := filepath.Join(lakeDir, "session_*.parquet")
	
	// Create a query that reads from all Parquet files using glob pattern
	baseSQL := fmt.Sprintf(`
		SELECT ts, source, level, message, id 
		FROM read_parquet('%s')`, parquetPattern)
	
	var rows *sql.Rows
	if query == "*" {
		// Show all logs with pagination
		fullSQL := baseSQL + ` ORDER BY ts DESC LIMIT ? OFFSET ?`
		rows, err = db.Query(fullSQL, limit, offset)
	} else {
		// Search with filter and pagination
		fullSQL := baseSQL + ` WHERE message LIKE ? OR source LIKE ? ORDER BY ts DESC LIMIT ? OFFSET ?`
		queryPattern := "%" + query + "%"
		rows, err = db.Query(fullSQL, queryPattern, queryPattern, limit, offset)
	}
	if err != nil {
		// Check if no parquet files exist yet
		matches, globErr := filepath.Glob(parquetPattern)
		if globErr == nil && len(matches) == 0 {
			return []*LogEntry{}, nil
		}
		return nil, fmt.Errorf("failed to query parquet files: %v", err)
	}
	defer rows.Close()

	var results []*LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.Ts, &e.Source, &e.Level, &e.Message, &e.ID); err != nil {
			return nil, err
		}
		results = append(results, &e)
	}
	
	return results, nil
}

// Session management functions

// GetActiveSessions returns information about currently active sessions
func GetActiveSessions() map[string]SessionInfo {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()
	
	result := make(map[string]SessionInfo)
	for id, db := range activeSessions {
		result[id] = SessionInfo{
			SessionID:    id,
			CreatedAt:    db.createdAt,
			LastActivity: db.lastActivity,
			ProcessInfo:  db.processInfo,
		}
	}
	return result
}

// SessionInfo contains information about a session
type SessionInfo struct {
	SessionID    string
	CreatedAt    time.Time
	LastActivity time.Time
	ProcessInfo  string
}

// CleanupInactiveSessions removes sessions that haven't been active for a specified duration
func CleanupInactiveSessions(maxInactiveTime time.Duration) int {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	
	now := time.Now()
	cleaned := 0
	
	for id, db := range activeSessions {
		if now.Sub(db.lastActivity) > maxInactiveTime {
			log.Printf("[INFO] Cleaning up inactive session: %s (inactive for %v)", id, now.Sub(db.lastActivity))
			db.Close()
			delete(activeSessions, id)
			cleaned++
		}
	}
	
	if cleaned > 0 {
		log.Printf("[INFO] Cleaned up %d inactive sessions. Active sessions: %d", cleaned, len(activeSessions))
	}
	
	return cleaned
}

// CloseSession closes a specific session and removes it from tracking
func (db *DB) CloseSession() error {
	// Always flush any remaining data before closing, even if pendingEntries is wrong
	// Check if there's actually data in the memory table
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
	if err != nil {
		log.Printf("[WARN] Failed to check remaining entries on close: %v", err)
	} else if count > 0 {
		log.Printf("[INFO] Flushing %d remaining entries on session close", count)
		if err := db.flushToParquet(); err != nil {
			log.Printf("[ERROR] Failed to flush remaining data on close: %v", err)
		}
	}
	
	if db.sessionID != "" {
		sessionsMutex.Lock()
		delete(activeSessions, db.sessionID)
		sessionsMutex.Unlock()
		
		log.Printf("[INFO] Session %s closed. Active sessions: %d", db.sessionID, len(activeSessions))
	}
	
	return db.Close()
}

