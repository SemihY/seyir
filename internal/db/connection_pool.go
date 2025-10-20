package db

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
)

// ConnectionPool manages a pool of DuckDB connections for high-performance operations
type ConnectionPool struct {
	connections chan *sql.DB
	maxSize     int
	createFunc  func() (*sql.DB, error)
	closeFunc   func(*sql.DB)
	mutex       sync.RWMutex
	closed      bool
	
	// Statistics
	totalConnections int64
	activeConnections int64
	peakConnections  int64
}

var (
	globalConnectionPool *ConnectionPool
	poolOnce             sync.Once
)

// GetConnectionPool returns the global connection pool
func GetConnectionPool() *ConnectionPool {
	poolOnce.Do(func() {
		globalConnectionPool = NewConnectionPool(10) // Max 10 connections
	})
	return globalConnectionPool
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(maxSize int) *ConnectionPool {
	pool := &ConnectionPool{
		connections: make(chan *sql.DB, maxSize),
		maxSize:     maxSize,
		createFunc: func() (*sql.DB, error) {
			db, err := sql.Open("duckdb", ":memory:")
			if err != nil {
				return nil, err
			}
			
			// Initialize the database with our schema
			_, err = db.Exec(`
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
					tags TEXT[]
				)
			`)
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("failed to create logs table: %v", err)
			}
			
			return db, nil
		},
		closeFunc: func(db *sql.DB) {
			db.Close()
		},
	}
	
	// Pre-warm the pool with initial connections
	go pool.preWarm()
	
	return pool
}

// preWarm creates initial connections in the pool
func (cp *ConnectionPool) preWarm() {
	for i := 0; i < cp.maxSize/2; i++ {
		conn, err := cp.createFunc()
		if err != nil {
			log.Printf("[WARN] Failed to pre-warm connection pool: %v", err)
			continue
		}
		
		select {
		case cp.connections <- conn:
			cp.totalConnections++
		default:
			cp.closeFunc(conn)
		}
	}
	
	log.Printf("[DEBUG] Connection pool pre-warmed with %d connections", len(cp.connections))
}

// Get retrieves a connection from the pool
func (cp *ConnectionPool) Get() (*sql.DB, error) {
	cp.mutex.RLock()
	if cp.closed {
		cp.mutex.RUnlock()
		return nil, fmt.Errorf("connection pool is closed")
	}
	cp.mutex.RUnlock()
	
	select {
	case conn := <-cp.connections:
		// Test the connection
		if err := conn.Ping(); err != nil {
			cp.closeFunc(conn)
			return cp.createNewConnection()
		}
		
		cp.activeConnections++
		if cp.activeConnections > cp.peakConnections {
			cp.peakConnections = cp.activeConnections
		}
		
		return conn, nil
		
	default:
		// Pool is empty, create a new connection
		return cp.createNewConnection()
	}
}

// createNewConnection creates a new database connection
func (cp *ConnectionPool) createNewConnection() (*sql.DB, error) {
	conn, err := cp.createFunc()
	if err != nil {
		return nil, fmt.Errorf("failed to create new connection: %v", err)
	}
	
	cp.totalConnections++
	cp.activeConnections++
	if cp.activeConnections > cp.peakConnections {
		cp.peakConnections = cp.activeConnections
	}
	
	return conn, nil
}

// Put returns a connection to the pool
func (cp *ConnectionPool) Put(conn *sql.DB) {
	if conn == nil {
		return
	}
	
	cp.mutex.RLock()
	if cp.closed {
		cp.mutex.RUnlock()
		cp.closeFunc(conn)
		return
	}
	cp.mutex.RUnlock()
	
	cp.activeConnections--
	
	// Test connection health before putting back
	if err := conn.Ping(); err != nil {
		cp.closeFunc(conn)
		return
	}
	
	// Clear the table for next use
	conn.Exec("DELETE FROM logs")
	
	select {
	case cp.connections <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close the connection
		cp.closeFunc(conn)
	}
}

// Close closes all connections in the pool
func (cp *ConnectionPool) Close() {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()
	
	if cp.closed {
		return
	}
	
	cp.closed = true
	close(cp.connections)
	
	// Close all remaining connections
	for conn := range cp.connections {
		cp.closeFunc(conn)
	}
	
	log.Printf("[INFO] Connection pool closed (total connections created: %d, peak: %d)", 
		cp.totalConnections, cp.peakConnections)
}

// GetStats returns pool statistics
func (cp *ConnectionPool) GetStats() PoolStats {
	cp.mutex.RLock()
	defer cp.mutex.RUnlock()
	
	return PoolStats{
		PoolSize:          len(cp.connections),
		MaxSize:           cp.maxSize,
		ActiveConnections: cp.activeConnections,
		TotalConnections:  cp.totalConnections,
		PeakConnections:   cp.peakConnections,
	}
}

// PoolStats contains connection pool statistics
type PoolStats struct {
	PoolSize          int   `json:"pool_size"`
	MaxSize           int   `json:"max_size"`
	ActiveConnections int64 `json:"active_connections"`
	TotalConnections  int64 `json:"total_connections"`
	PeakConnections   int64 `json:"peak_connections"`
}