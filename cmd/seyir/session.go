package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Session represents an active logging session that can have multiple processes
type Session struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	StartTime   time.Time             `json:"start_time"`
	Processes   map[string]*ProcessInfo `json:"processes"`
	Active      bool                  `json:"active"`
	LastUpdate  time.Time             `json:"last_update"`
	mutex       sync.RWMutex
}

// ProcessInfo tracks information about a process attached to a session
type ProcessInfo struct {
	Name         string    `json:"name"`
	StartTime    time.Time `json:"start_time"`
	LogCount     int64     `json:"log_count"`
	LastSeen     time.Time `json:"last_seen"`
	ErrorCount   int64     `json:"error_count"`
	WarningCount int64     `json:"warning_count"`
}

// SessionManager handles global session state
type SessionManager struct {
	sessions map[string]*Session
	mutex    sync.RWMutex
	dataDir  string
}

var globalSessionManager *SessionManager
var sessionManagerOnce sync.Once

// GetSessionManager returns the global session manager instance
func GetSessionManager() *SessionManager {
	sessionManagerOnce.Do(func() {
		dataDir := getDataDir()
		globalSessionManager = &SessionManager{
			sessions: make(map[string]*Session),
			dataDir:  dataDir,
		}
		// Load existing sessions from disk
		globalSessionManager.loadSessions()
	})
	return globalSessionManager
}

// generateSessionID creates a unique session ID
func generateSessionID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return fmt.Sprintf("sess_%s", hex.EncodeToString(bytes))
}

// CreateSession creates a new session or returns existing one with the same name
func (sm *SessionManager) CreateSession(name string) *Session {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	// Check if session with this name already exists and is active
	for _, session := range sm.sessions {
		if session.Name == name && session.Active {
			session.LastUpdate = time.Now()
			sm.saveSession(session)
			return session
		}
	}
	
	session := &Session{
		ID:         generateSessionID(),
		Name:       name,
		StartTime:  time.Now(),
		Processes:  make(map[string]*ProcessInfo),
		Active:     true,
		LastUpdate: time.Now(),
	}
	
	sm.sessions[session.ID] = session
	sm.saveSession(session)
	
	return session
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(id string) (*Session, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	session, exists := sm.sessions[id]
	return session, exists
}

// GetSessionByName retrieves an active session by name
func (sm *SessionManager) GetSessionByName(name string) (*Session, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	for _, session := range sm.sessions {
		if session.Name == name && session.Active {
			return session, true
		}
	}
	return nil, false
}

// GetActiveSessions returns all active sessions
func (sm *SessionManager) GetActiveSessions() []*Session {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	var activeSessions []*Session
	for _, session := range sm.sessions {
		if session.Active {
			activeSessions = append(activeSessions, session)
		}
	}
	return activeSessions
}

// AttachProcess attaches a process to a session
func (s *Session) AttachProcess(processName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if _, exists := s.Processes[processName]; !exists {
		s.Processes[processName] = &ProcessInfo{
			Name:         processName,
			StartTime:    time.Now(),
			LogCount:     0,
			LastSeen:     time.Now(),
			ErrorCount:   0,
			WarningCount: 0,
		}
		s.LastUpdate = time.Now()
		
		// Save session state to disk
		GetSessionManager().saveSession(s)
	}
}

// UpdateProcessStats updates statistics for a process
func (s *Session) UpdateProcessStats(processName, level string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if proc, exists := s.Processes[processName]; exists {
		proc.LogCount++
		proc.LastSeen = time.Now()
		
		switch level {
		case "ERROR", "FATAL":
			proc.ErrorCount++
		case "WARN", "WARNING":
			proc.WarningCount++
		}
		
		s.LastUpdate = time.Now()
	}
}

// GetProcessNames returns all process names in the session
func (s *Session) GetProcessNames() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	var names []string
	for name := range s.Processes {
		names = append(names, name)
	}
	return names
}

// GetStats returns session statistics
func (s *Session) GetStats() SessionStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	var totalLogs, totalErrors, totalWarnings int64
	var processCount int
	var lastActivity time.Time
	
	for _, proc := range s.Processes {
		totalLogs += proc.LogCount
		totalErrors += proc.ErrorCount
		totalWarnings += proc.WarningCount
		processCount++
		
		if proc.LastSeen.After(lastActivity) {
			lastActivity = proc.LastSeen
		}
	}
	
	return SessionStats{
		ProcessCount:  processCount,
		TotalLogs:     totalLogs,
		ErrorCount:    totalErrors,
		WarningCount:  totalWarnings,
		LastActivity:  lastActivity,
		Duration:      time.Since(s.StartTime),
	}
}

// SessionStats represents session statistics
type SessionStats struct {
	ProcessCount  int           `json:"process_count"`
	TotalLogs     int64         `json:"total_logs"`
	ErrorCount    int64         `json:"error_count"`
	WarningCount  int64         `json:"warning_count"`
	LastActivity  time.Time     `json:"last_activity"`
	Duration      time.Duration `json:"duration"`
}

// Stop deactivates the session
func (s *Session) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.Active = false
	s.LastUpdate = time.Now()
	
	GetSessionManager().saveSession(s)
}

// saveSession persists session to disk
func (sm *SessionManager) saveSession(session *Session) {
	sessionDir := filepath.Join(sm.dataDir, "sessions")
	os.MkdirAll(sessionDir, 0755)
	
	sessionFile := filepath.Join(sessionDir, fmt.Sprintf("%s.json", session.ID))
	
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return // Silent fail for now
	}
	
	os.WriteFile(sessionFile, data, 0644)
}

// loadSessions loads all sessions from disk
func (sm *SessionManager) loadSessions() {
	sessionDir := filepath.Join(sm.dataDir, "sessions")
	
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return // Directory doesn't exist yet
	}
	
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		
		sessionFile := filepath.Join(sessionDir, entry.Name())
		data, err := os.ReadFile(sessionFile)
		if err != nil {
			continue
		}
		
		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		
		// Deactivate sessions older than 1 hour
		if time.Since(session.LastUpdate) > time.Hour {
			session.Active = false
		}
		
		sm.sessions[session.ID] = &session
	}
}

// CleanupInactiveSessions removes old inactive sessions
func (sm *SessionManager) CleanupInactiveSessions(maxAge time.Duration) int {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	var cleaned int
	sessionDir := filepath.Join(sm.dataDir, "sessions")
	
	for id, session := range sm.sessions {
		if !session.Active && time.Since(session.LastUpdate) > maxAge {
			delete(sm.sessions, id)
			
			// Remove file
			sessionFile := filepath.Join(sessionDir, fmt.Sprintf("%s.json", id))
			os.Remove(sessionFile)
			
			cleaned++
		}
	}
	
	return cleaned
}