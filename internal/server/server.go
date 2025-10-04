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
		log.Fatal("Failed to get executable path:", err)
	}
	execDir := filepath.Dir(execPath)
	
	// Try multiple locations for UI files
	var uiDir string
	candidates := []string{
		"ui",                                    // Current working directory
		filepath.Join(execDir, "ui"),           // Next to executable
		filepath.Join(execDir, "..", "ui"),     // Development mode
	}
	
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			uiDir = candidate
			break
		}
	}
	
	if uiDir == "" {
		log.Fatal("Could not find ui directory")
	}
	
	// Set up routes
	http.Handle("/", http.FileServer(http.Dir(uiDir)))
	http.HandleFunc("/api/sse", tail.SSEHandler)
	// TODO: Add query endpoints for search functionality
	
	log.Fatal(http.ListenAndServe(addr, nil))
}