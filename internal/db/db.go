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
)

// DB wraps sql.DB to provide a type in the core package
type DB struct {
	*sql.DB
}

// SetGlobalLakeDir sets the global lake directory where all session DBs are stored
func SetGlobalLakeDir(dir string) {
	lakeDirMutex.Lock()
	defer lakeDirMutex.Unlock()
	globalLakeDir = dir
}

// GetGlobalLakeDir returns the global lake directory
func GetGlobalLakeDir() string {
	lakeDirMutex.RLock()
	defer lakeDirMutex.RUnlock()
	return globalLakeDir
}

// generateSessionID creates a unique session ID for this pipe operation
func generateSessionID() string {
	return fmt.Sprintf("session_%d_%d", time.Now().Unix(), os.Getpid())
}

// NewSessionConnection creates a new DuckDB instance with unique session ID
// Each pipe operation gets its own DuckDB file connected to shared DuckLake
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
	sessionDBPath := filepath.Join(lakeDir, fmt.Sprintf("logs_%s.duckdb", sessionID))
	
	log.Printf("[INFO] Creating new session DB: %s", sessionDBPath)

	// Create new DuckDB instance for this session
	db, err := sql.Open("duckdb", sessionDBPath)
	if err != nil { 
		return nil, fmt.Errorf("failed to create session database %s: %v", sessionDBPath, err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to session database: %v", err)
	}

	// Initialize database wrapper
	dbWrapper := &DB{db}

	// Create shared metadata database for federation (no extensions needed)
	metadataDBPath := filepath.Join(lakeDir, "metadata.duckdb")
	
	// Try to attach shared metadata database for federation
	attachSQL := fmt.Sprintf("ATTACH '%s' AS metadata;", metadataDBPath)
	if _, err := dbWrapper.Exec(attachSQL); err != nil {
		log.Printf("[WARN] Failed to attach metadata database: %v", err)
		// Continue without federation - each session will be independent
	} else {
		log.Printf("[INFO] Attached to shared metadata database: %s", metadataDBPath)
		
		// Ensure metadata tables exist (compatible with DuckDB v1.3.0)
		_, err := dbWrapper.Exec(`
			CREATE TABLE IF NOT EXISTS metadata.session_registry (
				session_id TEXT NOT NULL,
				db_path TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL,
				last_active TIMESTAMP NOT NULL
			)
		`)
		if err != nil {
			log.Printf("[WARN] Failed to create session registry: %v", err)
		} else {
			// Register this session
			now := time.Now()
			_, err = dbWrapper.Exec(`
				INSERT INTO metadata.session_registry (session_id, db_path, created_at, last_active) 
				VALUES (?, ?, ?, ?)
			`, sessionID, sessionDBPath, now, now)
			if err != nil {
				log.Printf("[WARN] Failed to register session: %v", err)
			}
		}
	}

	// Ensure proper database schema
	if err := dbWrapper.EnsureSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database schema: %v", err)
	}

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
	dbWrapper := &DB{db}

	// Ensure proper database schema
	if err := dbWrapper.EnsureSchema(); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	return dbWrapper
}

// EnsureSchema creates the necessary tables and indexes if they don't exist
func (db *DB) EnsureSchema() error {
	// Create logs table compatible with DuckDB v1.3.0 (no DEFAULT CURRENT_TIMESTAMP or PRIMARY KEY)
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

	// Create indexes for better query performance
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts)`)
	if err != nil {
		log.Printf("[WARN] Failed to create timestamp index: %v", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_logs_source ON logs(source)`)
	if err != nil {
		log.Printf("[WARN] Failed to create source index: %v", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_logs_level ON logs(level)`)
	if err != nil {
		log.Printf("[WARN] Failed to create level index: %v", err)
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
	_, err := db.Exec(`INSERT INTO logs VALUES (?, ?, ?, ?, ?)`, e.Ts, e.Source, e.Level, e.Message, e.ID)
	if err != nil { log.Printf("[ERROR] failed to save log: %v", err) }
}
