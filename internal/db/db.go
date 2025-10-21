package db

import (
	"seyir/internal/logger"
	"sync"
	"time"
)

var (
	// Global lake directory where all parquet files are stored
	globalLakeDir string
	lakeDirMutex  sync.RWMutex
	
	// Active sessions tracking (for legacy compatibility)
	activeSessions map[string]*SessionInfo
	sessionsMutex  sync.RWMutex
)

// SetGlobalLakeDir sets the global lake directory where all parquet files are stored
func SetGlobalLakeDir(dir string) {
	lakeDirMutex.Lock()
	defer lakeDirMutex.Unlock()
	globalLakeDir = dir
	
	// Initialize session tracking if not already done
	if activeSessions == nil {
		activeSessions = make(map[string]*SessionInfo)
	}
}

// GetGlobalLakeDir returns the global lake directory
func GetGlobalLakeDir() string {
	lakeDirMutex.RLock()
	defer lakeDirMutex.RUnlock()
	return globalLakeDir
}

// SessionInfo contains information about a session
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
	ProcessInfo  string    `json:"process_info"`
}

// GetActiveSessions returns information about currently active sessions
func GetActiveSessions() map[string]SessionInfo {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()
	
	result := make(map[string]SessionInfo)
	for id, info := range activeSessions {
		result[id] = *info
	}
	return result
}

// CleanupInactiveSessions removes sessions that haven't been active for a specified duration
func CleanupInactiveSessions(maxInactiveTime time.Duration) int {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	
	now := time.Now()
	cleaned := 0
	
	for id, session := range activeSessions {
		if now.Sub(session.LastActivity) > maxInactiveTime {
			logger.Info("Cleaning up inactive session: %s (inactive for %v)", id, now.Sub(session.LastActivity))
			delete(activeSessions, id)
			cleaned++
		}
	}
	
	if cleaned > 0 {
		logger.Info("Cleaned up %d inactive sessions. Active sessions: %d", cleaned, len(activeSessions))
	}
	
	return cleaned
}
