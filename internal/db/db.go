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
	sessionID    string    // Session ID for this pipe operation
	parquetPath  string    // Path to the Parquet file for this session
	createdAt    time.Time // When this session was created
	lastActivity time.Time // Last time this session was used
	processInfo  string    // Information about the process using this session
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
		DB:           db,
		sessionID:    sessionID,
		parquetPath:  filepath.Join(lakeDir, fmt.Sprintf("logs_%s.parquet", sessionID)),
		createdAt:    now,
		lastActivity: now,
		processInfo:  processInfo,
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

// SaveLogDirectly saves a log entry directly to a Parquet file
func (db *DB) SaveLogDirectly(e *LogEntry) error {
	if db.sessionID == "" {
		return fmt.Errorf("no session ID set for this database")
	}
	
	// Update last activity time
	db.lastActivity = time.Now()
	
	// Get the lake directory
	lakeDir := GetGlobalLakeDir()
	if lakeDir == "" {
		return fmt.Errorf("global lake directory not set")
	}
	
	// Create a unique filename for this log entry (timestamp-based for ordering)
	timestamp := time.Now().UnixNano()
	parquetFile := filepath.Join(lakeDir, fmt.Sprintf("logs_%s_%d.parquet", db.sessionID, timestamp))
	
	// Create a temporary table with just this entry
	tempTableName := fmt.Sprintf("temp_%d", timestamp)
	
	// Create temp table with the same structure as logs
	_, err := db.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			ts TIMESTAMP NOT NULL,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			id TEXT NOT NULL
		)`, tempTableName))
	if err != nil {
		return fmt.Errorf("failed to create temp table: %v", err)
	}
	
	// Insert the entry into temp table
	_, err = db.Exec(fmt.Sprintf("INSERT INTO %s VALUES (?, ?, ?, ?, ?)", tempTableName), 
		e.Ts, e.Source, e.Level, e.Message, e.ID)
	if err != nil {
		return fmt.Errorf("failed to insert into temp table: %v", err)
	}
	
	// Export temp table to its own Parquet file
	exportSQL := fmt.Sprintf("COPY %s TO '%s' (FORMAT PARQUET)", tempTableName, parquetFile)
	_, err = db.Exec(exportSQL)
	if err != nil {
		return fmt.Errorf("failed to export to parquet file %s: %v", parquetFile, err)
	}
	
	// Clean up temp table
	_, err = db.Exec(fmt.Sprintf("DROP TABLE %s", tempTableName))
	if err != nil {
		log.Printf("[WARN] Failed to drop temp table: %v", err)
	}
	
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

	// Find all Parquet files in the lake directory (including timestamped files)
	parquetPattern := filepath.Join(lakeDir, "logs_*.parquet")
	
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
	if db.sessionID != "" {
		sessionsMutex.Lock()
		delete(activeSessions, db.sessionID)
		sessionsMutex.Unlock()
		
		log.Printf("[INFO] Session %s closed. Active sessions: %d", db.sessionID, len(activeSessions))
	}
	
	return db.Close()
}

