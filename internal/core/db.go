package core

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "github.com/marcboeker/go-duckdb"
)

// DB wraps sql.DB to provide a type in the core package
type DB struct {
	*sql.DB
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
	// Create logs table with proper indexing for better performance
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS logs (
			ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			id TEXT NOT NULL PRIMARY KEY
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
