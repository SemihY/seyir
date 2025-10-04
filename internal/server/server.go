package server

import (
	"log"
	"logspot/internal/tail"
	"net/http"
	"os"
	"path/filepath"
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
		"/usr/local/share/logspot/ui",                    // System-wide UI location
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
	// TODO: Add query endpoints for search functionality
	
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