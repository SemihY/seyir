package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"seyir/internal/collector"
	"seyir/internal/db"
	"seyir/internal/server"
	"strings"
	"syscall"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

func getDataDir() string {
	// Try to get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current working directory
		return ".seyir"
	}
	return filepath.Join(homeDir, ".seyir")
}

// showUsage displays usage information and examples
func showUsage() {
	fmt.Println(`seyir - Centralized Log Collector & Viewer

Usage:
    seyir <command> [flags]
    command | seyir                        # Pipe mode

Commands:
    service     Start log collection service (Docker containers + Web UI)
    web         Start web server only 
    search      Search collected logs
    sessions    Show active log sessions
    cleanup     Clean up old log sessions
    help        Show this help

Flags:`)
	flag.PrintDefaults()
	
	fmt.Println(`
Examples:
    # Service mode (auto-discovers containers)
    seyir service --port 8080

    # Web interface only
    seyir web --port 8080

    # Search logs  
    seyir search --search "error" --limit 50
    seyir search --search "trace_id=abc123"
    seyir search --search "*" --limit 10

    # Pipe logs directly
    docker logs mycontainer | seyir
    kubectl logs -f deployment/api | seyir

    # Management
    seyir sessions

Container Setup:
    # Containers opt-in to tracking with labels:
    docker run -l seyir.enable=true my-app
    docker run -l seyir.enable=true -l seyir.project=web -l seyir.component=api backend-service
    seyir cleanup

Docker Deployment:
    docker run -d \\
      -v /var/run/docker.sock:/var/run/docker.sock \\
      -v seyir-data:/app/data \\
      -p 8080:8080 \\
      seyir:latest

Architecture:
    - Auto-discovers containers with 'seyir.enable=true' label
    - Containers self-register with optional project/component metadata
    - Parses structured logs (JSON, key-value, timestamps)
    - Stores in compressed Parquet format
    - Provides unified web interface for all logs
    - Fast search across trace IDs, processes, components

Data: ~/.seyir/lake/`)
}

var (
	port        = flag.String("port", "8080", "Port for web server")
	searchQuery = flag.String("search", "", "Search logs (use '*' for all logs)")
	limit       = flag.Int("limit", 100, "Search result limit")
)

func main() {
	flag.Parse()
	
	// Set up the lake directory where all session DBs will be stored
	dataDir := getDataDir()
	lakeDir := filepath.Join(dataDir, "lake")
	db.SetGlobalLakeDir(lakeDir)
	
	args := flag.Args()
	
	// If no command provided, check for pipe mode or show help
	if len(args) == 0 {
		if isPipeOperation() {
			runPipeMode()
			return
		}
		showUsage()
		return
	}
	
	command := args[0]
	
	switch command {
	case "service":
		runServiceMode(*port)
	case "web":
		runWebServer(*port)
	case "search":
		if *searchQuery == "" {
			fmt.Println("Error: --search flag is required for search command")
			os.Exit(1)
		}
		runSearchMode(*searchQuery, *limit)
	case "sessions":
		runShowSessions()
	case "cleanup":
		runCleanupSessions()
	case "help", "--help", "-h":
		showUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		showUsage()
		os.Exit(1)
	}
}

// isPipeOperation checks if stdin has piped data
func isPipeOperation() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// Check if stdin is not a character device (i.e., it's piped data)
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// runPipeMode handles a single pipe operation - creates session DB and exits
func runPipeMode() {
	log.Printf("[INFO] Running in pipe mode - creating session DB")

	collectorManager := collector.NewManager()

	// Enable stdin collector (creates its own session DB)
	if err := collectorManager.EnableStdin("stdin"); err != nil {
		fmt.Printf("Failed to enable stdin collector: %v\n", err)
		os.Exit(1)
	}

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)

	// Wait for the collector to finish naturally or for a signal
	done := make(chan bool, 1)
	go func() {
		// Monitor the stdin collector until it's no longer healthy (finished)
		for {
			time.Sleep(100 * time.Millisecond)
			stdinCollector, exists := collectorManager.GetCollector("stdin")
			if !exists {
				done <- true
				return
			}
			if !stdinCollector.IsHealthy() {
				done <- true
				return
			}
		}
	}()

	// Wait for either completion or signal
	select {
	case <-sigChan:
		log.Printf("[INFO] Received signal, shutting down")
	case <-done:
		log.Printf("[INFO] Stdin collection completed")
	}

	// Stop collectors and exit
	collectorManager.StopAll()
	log.Printf("[INFO] Pipe operation completed")
}

// runSearchMode searches across all session databases and outputs results
func runSearchMode(query string, limit int) {
	fmt.Printf("Searching for: %s (limit: %d)\n", query, limit)
	
	// search with db.searchLogs
	results, err := db.SearchLogs(query, limit)
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	
	// Display results
	fmt.Printf("\nResults:\n")
	fmt.Printf("%-20s %-15s %-8s %s\n", "TIMESTAMP", "SOURCE", "LEVEL", "MESSAGE")
	fmt.Printf("%s\n", strings.Repeat("-", 80))

	count := 0
	for _, entry := range results {
		fmt.Printf("%-20s %-15s %-8s %s\n", entry.Ts, entry.Source, entry.Level, entry.Message)
		count++
	}
	fmt.Printf("\nFound %d matching log entries\n", count)
}

// runShowSessions displays information about active sessions
func runShowSessions() {
	sessions := db.GetActiveSessions()
	
	if len(sessions) == 0 {
		fmt.Println("No active sessions found.")
		return
	}
	
	fmt.Printf("Active Sessions (%d):\n", len(sessions))
	fmt.Printf("%-25s %-20s %-20s %s\n", "SESSION ID", "CREATED", "LAST ACTIVITY", "PROCESS")
	fmt.Printf("%s\n", strings.Repeat("-", 100))
	
	for _, session := range sessions {
		createdStr := session.CreatedAt.Format("15:04:05")
		activityStr := session.LastActivity.Format("15:04:05")
		
		fmt.Printf("%-25s %-20s %-20s %s\n", 
			session.SessionID, createdStr, activityStr, session.ProcessInfo)
	}
}

// runCleanupSessions removes inactive sessions
func runCleanupSessions() {
	// Clean up sessions inactive for more than 5 minutes
	cleaned := db.CleanupInactiveSessions(5 * time.Minute)
	
	if cleaned > 0 {
		fmt.Printf("Cleaned up %d inactive sessions.\n", cleaned)
	} else {
		fmt.Println("No inactive sessions to clean up.")
	}
	
	// Show remaining active sessions
	sessions := db.GetActiveSessions()
	fmt.Printf("Active sessions remaining: %d\n", len(sessions))
}

// runWebServer starts the web server for log viewing
func runWebServer(port string) {
	srv := server.New(port)
	if err := srv.Start(); err != nil {
		fmt.Printf("Failed to start web server: %v\n", err)
		os.Exit(1)
	}
}

// runServiceMode starts both Docker collection and web server
func runServiceMode(port string) {
	log.Printf("[INFO] 🚀 Starting seyir service")
	log.Printf("[INFO] 🌐 Web interface: http://localhost:%s", port)
	log.Printf("[INFO] 🔍 Auto-discovering containers with 'seyir.enable=true' label")

	// Create collector manager
	collectorManager := collector.NewManager()

	// Create and add Docker collector (no filters needed, uses opt-in labels)
	dockerCollector := collector.NewDockerCollector()
	if dockerCollector == nil {
		fmt.Printf("❌ Failed to create Docker collector\n")
		os.Exit(1)
	}

	if err := collectorManager.AddCollector("docker", dockerCollector); err != nil {
		fmt.Printf("❌ Failed to add Docker collector: %v\n", err)
		os.Exit(1)
	}

	// Start collectors
	if err := collectorManager.StartAll(); err != nil {
		fmt.Printf("❌ Failed to start collectors: %v\n", err)
		os.Exit(1)
	}

	// Start web server in a goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		srv := server.New(port)
		serverErrChan <- srv.Start()
	}()

	log.Printf("[INFO] ✅ seyir service running. Press Ctrl+C to stop.")

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrChan:
		fmt.Printf("❌ Web server failed: %v\n", err)
		collectorManager.StopAll()
		os.Exit(1)
	case <-sigChan:
		log.Printf("[INFO] 🛑 Shutting down seyir service...")
		collectorManager.StopAll()
	}
}

