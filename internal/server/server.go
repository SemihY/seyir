package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"logspot/internal/db"
	"logspot/internal/tail"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// StartWebServer starts the HTTP server for the web UI
func StartWebServer(addr string) {
	// Get the directory where the executable is located
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: Failed to get executable path: %v", err)
	}
	
	var uiDir string
	var hasUI bool
	
	// Try multiple locations for UI files
	candidates := []string{
		"ui",                                              // Current working directory
		filepath.Join(filepath.Dir(execPath), "ui"),      // Next to executable
		filepath.Join(filepath.Dir(execPath), "..", "ui"), // Development mode
		"/usr/local/share/logspot/ui",                    // System-wide UI location (installed)
		filepath.Join(os.Getenv("HOME"), ".logspot", "ui"), // User's home directory
	}
	
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			uiDir = candidate
			hasUI = true
			break
		}
	}
	
	// Set up routes
	if hasUI {
		log.Printf("Serving UI from: %s", uiDir)
		http.Handle("/", http.FileServer(http.Dir(uiDir)))
	} else {
		log.Printf("Warning: UI directory not found, serving minimal interface")
		http.HandleFunc("/", serveMinimalUI)
	}
	
	http.HandleFunc("/api/sse", tail.SSEHandler)
	http.HandleFunc("/api/query", handleQuery)
	http.HandleFunc("/api/sources", handleSources)
	
	log.Printf("Web server starting on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// serveMinimalUI provides a basic HTML interface when UI files are not found
func serveMinimalUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Logspot</title>
    <style>
        body { font-family: monospace; background: #0d1117; color: #c9d1d9; margin: 20px; }
        .header { border-bottom: 1px solid #30363d; padding-bottom: 10px; margin-bottom: 20px; }
        .log-entry { padding: 5px 0; border-bottom: 1px solid #21262d; }
        .error { color: #f85149; }
        .warn { color: #d29922; }
        .info { color: #58a6ff; }
        #logs { height: 70vh; overflow-y: auto; border: 1px solid #30363d; padding: 10px; }
    </style>
</head>
<body>
    <div class="header">
        <h1>🪶 Logspot</h1>
        <p>Real-time log viewer - Basic interface (UI files not found)</p>
    </div>
    <div id="logs"></div>
    <script>
        const logsContainer = document.getElementById('logs');
        const eventSource = new EventSource('/api/sse');
        
        eventSource.onmessage = function(event) {
            try {
                const data = JSON.parse(event.data);
                if (data.type === 'log') {
                    const logEntry = document.createElement('div');
                    logEntry.className = 'log-entry ' + data.data.level.toLowerCase();
                    logEntry.textContent = '[' + data.data.level + '] ' + data.data.source + ': ' + data.data.message;
                    logsContainer.appendChild(logEntry);
                    logsContainer.scrollTop = logsContainer.scrollHeight;
                }
            } catch (e) {
                console.error('Error parsing SSE data:', e);
            }
        };
        
        eventSource.onerror = function(event) {
            console.error('SSE connection error:', event);
        };
    </script>
</body>
</html>
    `))
}

// LogEntry represents a log entry for API responses
type LogEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// handleQuery federates queries across all session databases in the lake
func handleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	query := r.URL.Query().Get("q")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}
	
	// Get all session databases from the lake directory
	lakeDir := db.GetGlobalLakeDir()
	if lakeDir == "" {
		http.Error(w, "Lake directory not configured", http.StatusInternalServerError)
		return
	}
	
	sessionFiles, err := filepath.Glob(filepath.Join(lakeDir, "logs_session_*.duckdb"))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to find session databases: %v", err), http.StatusInternalServerError)
		return
	}
	
	if len(sessionFiles) == 0 {
		json.NewEncoder(w).Encode([]LogEntry{})
		return
	}
	
	// Create temporary DuckDB instance for federation
	tempDB, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create temp database: %v", err), http.StatusInternalServerError)
		return
	}
	defer tempDB.Close()
	
	// Attach all session databases
	var unionParts []string
	for i, sessionFile := range sessionFiles {
		dbAlias := fmt.Sprintf("session_%d", i)
		attachSQL := fmt.Sprintf("ATTACH '%s' AS %s", sessionFile, dbAlias)
		if _, err := tempDB.Exec(attachSQL); err != nil {
			log.Printf("[WARN] Failed to attach %s: %v", sessionFile, err)
			continue
		}
		unionParts = append(unionParts, fmt.Sprintf("SELECT * FROM %s.logs", dbAlias))
	}
	
	if len(unionParts) == 0 {
		json.NewEncoder(w).Encode([]LogEntry{})
		return
	}
	
	// Build federated query
	federatedQuery := strings.Join(unionParts, " UNION ALL ")
	if query != "" {
		federatedQuery = fmt.Sprintf("SELECT * FROM (%s) WHERE message LIKE '%%%s%%' OR source LIKE '%%%s%%'", 
			federatedQuery, query, query)
	}
	federatedQuery += fmt.Sprintf(" ORDER BY ts DESC LIMIT %s", limit)
	
	rows, err := tempDB.Query(federatedQuery)
	if err != nil {
		http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	
	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		if err := rows.Scan(&entry.Timestamp, &entry.Source, &entry.Level, &entry.Message, &entry.ID); err != nil {
			log.Printf("[ERROR] Failed to scan row: %v", err)
			continue
		}
		logs = append(logs, entry)
	}
	
	json.NewEncoder(w).Encode(logs)
}

// handleSources lists all active session databases in the lake
func handleSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	lakeDir := db.GetGlobalLakeDir()
	if lakeDir == "" {
		http.Error(w, "Lake directory not configured", http.StatusInternalServerError)
		return
	}
	
	sessionFiles, err := filepath.Glob(filepath.Join(lakeDir, "logs_session_*.duckdb"))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to find session databases: %v", err), http.StatusInternalServerError)
		return
	}
	
	sources := make([]map[string]interface{}, 0, len(sessionFiles))
	for _, sessionFile := range sessionFiles {
		info, err := os.Stat(sessionFile)
		if err != nil {
			continue
		}
		
		sources = append(sources, map[string]interface{}{
			"id":          filepath.Base(sessionFile),
			"path":        sessionFile,
			"created_at":  info.ModTime(),
			"size_bytes":  info.Size(),
			"is_active":   true,
		})
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sources": sources,
		"count":   len(sources),
	})
}