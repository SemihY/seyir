package db

import (
	"log"
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

// RegisterSession registers a new session
func RegisterSession(sessionID, processInfo string) {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	
	activeSessions[sessionID] = &SessionInfo{
		SessionID:    sessionID,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		ProcessInfo:  processInfo,
	}
	
	log.Printf("[INFO] Session %s registered. Active sessions: %d", sessionID, len(activeSessions))
}

// UpdateSessionActivity updates the last activity time for a session
func UpdateSessionActivity(sessionID string) {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	
	if session, exists := activeSessions[sessionID]; exists {
		session.LastActivity = time.Now()
	}
}

// UnregisterSession removes a session from tracking
func UnregisterSession(sessionID string) {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	
	delete(activeSessions, sessionID)
	log.Printf("[INFO] Session %s unregistered. Active sessions: %d", sessionID, len(activeSessions))
}

// CleanupInactiveSessions removes sessions that haven't been active for a specified duration
func CleanupInactiveSessions(maxInactiveTime time.Duration) int {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	
	now := time.Now()
	cleaned := 0
	
	for id, session := range activeSessions {
		if now.Sub(session.LastActivity) > maxInactiveTime {
			log.Printf("[INFO] Cleaning up inactive session: %s (inactive for %v)", id, now.Sub(session.LastActivity))
			delete(activeSessions, id)
			cleaned++
		}
	}
	
	if cleaned > 0 {
		log.Printf("[INFO] Cleaned up %d inactive sessions. Active sessions: %d", cleaned, len(activeSessions))
	}
	
	return cleaned
}
