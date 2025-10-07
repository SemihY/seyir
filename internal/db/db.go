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
	sessionID    string       // Session ID for this pipe operation
	parquetPath  string       // Path to the Parquet file for this session
	createdAt    time.Time    // When this session was created
	lastActivity time.Time    // Last time this session was used
	processInfo  string       // Information about the process using this session
	batchBuffer  *BatchBuffer // Batch buffer for this session
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
		parquetPath:  filepath.Join(lakeDir, fmt.Sprintf("session_%s.parquet", sessionID)),
		createdAt:    now,
		lastActivity: now,
		processInfo:  processInfo,
	}

	// Initialize batch buffer with default configuration
	batchConfig := DefaultBatchConfig()
	dbWrapper.batchBuffer = NewBatchBuffer(dbWrapper, batchConfig)

	// Create temporary table for this session with extended schema
	_, err = dbWrapper.Exec(`
		CREATE TABLE logs (
			ts TIMESTAMP NOT NULL,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			id TEXT NOT NULL,
			trace_id TEXT,
			process TEXT,
			component TEXT,
			thread TEXT,
			user_id TEXT,
			request_id TEXT,
			tags TEXT[] -- Array for tags
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
	
	// For regular InitDB connections, create the logs table with extended schema
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS logs (
			ts TIMESTAMP NOT NULL,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			id TEXT NOT NULL,
			trace_id TEXT,
			process TEXT,
			component TEXT,
			thread TEXT,
			user_id TEXT,
			request_id TEXT,
			tags TEXT[] -- Array for tags
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

// convertTagsToArray converts a string slice to a format DuckDB can handle
func convertTagsToArray(tags []string) interface{} {
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func SaveLog(db *DB, e *LogEntry) {
	if err := db.SaveLogDirectly(e); err != nil {
		log.Printf("[ERROR] failed to save log: %v", err)
	}
}

// SaveLogDirectly saves a log entry using the batch buffer for efficiency
func (db *DB) SaveLogDirectly(e *LogEntry) error {
	if db.sessionID == "" {
		return fmt.Errorf("no session ID set for this database")
	}
	
	// Update last activity time
	db.lastActivity = time.Now()
	
	// Use batch buffer if available
	if db.batchBuffer != nil {
		return db.batchBuffer.Add(e)
	}
	
	return fmt.Errorf("batch buffer not available for session")
}

// ClearMemoryTable removes all entries from the in-memory logs table
func (db *DB) ClearMemoryTable() error {
	_, err := db.Exec("DELETE FROM logs")
	if err != nil {
		return fmt.Errorf("failed to clear memory table: %v", err)
	}
	return nil
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
	// Close batch buffer first to flush any remaining entries
	if db.batchBuffer != nil {
		if err := db.batchBuffer.Close(); err != nil {
			log.Printf("[ERROR] Failed to close batch buffer: %v", err)
		}
	}
	
	// Batch buffer handles all flushing, no need for legacy checks
	
	if db.sessionID != "" {
		sessionsMutex.Lock()
		delete(activeSessions, db.sessionID)
		sessionsMutex.Unlock()
		
		log.Printf("[INFO] Session %s closed. Active sessions: %d", db.sessionID, len(activeSessions))
	}
	
	return db.Close()
}

