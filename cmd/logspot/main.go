package main

import (
	"logspot/internal/collector"
	"logspot/internal/db"
	"logspot/internal/discovery"
	"logspot/internal/retention"
	"logspot/internal/server"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func getDataDir() string {
	// Try to get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current working directory
		return ".logspot"
	}
	return filepath.Join(homeDir, ".logspot")
}

func main() {
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "logs.duckdb")
	database := db.InitDB(dbPath)

	// Pipe / stdin mode
	go collector.CaptureStdin(database, "stdin")

	// Coolify / Docker auto discovery
	if os.Getenv("ENABLE_DOCKER_CAPTURE") == "true" {
		go discovery.StartDockerDiscovery(database)
	}

	// Retention
	go retention.RetentionCleaner(database, 7)

	// Web server
	go server.StartWebServer("localhost:7777")

	// Open browser automatically
	if os.Getenv("DISABLE_AUTO_OPEN") != "true" {
		go func() {
			// Wait a bit for the server to start
			// In a real app, you'd want a more robust way to ensure the server is ready
			// before opening the browser.
			// Here we just wait 2 seconds.
			// time.Sleep(2 * time.Second)
			openBrowser("http://localhost:7777")
		}()
	}

	// Keep the main process running
	select {} // Block forever
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin": cmd = "open"
	case "windows": cmd = "rundll32"; args = append(args,"url.dll,FileProtocolHandler")
	default: cmd="xdg-open"
	}
	args = append(args,url)
	exec.Command(cmd,args...).Start()
}

