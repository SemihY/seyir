package main

import (
	"flag"
	"fmt"
	"log"
	"logspot/internal/collector"
	"logspot/internal/db"
	"logspot/internal/server"
	"os"
	"os/signal"
	"path/filepath"
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
		return ".logspot"
	}
	return filepath.Join(homeDir, ".logspot")
}

// showUsage displays usage information and examples
func showUsage() {
	fmt.Println(`Logspot - Real-time Log Viewer

Usage:
    logspot [flags]                          # Interactive mode
    command | logspot                        # Pipe mode (creates session)
    logspot --search "query"                 # Search mode
    logspot --sessions                       # Show active sessions
    logspot --cleanup                        # Cleanup inactive sessions
    logspot --web                            # Start web server

Flags:`)
	flag.PrintDefaults()
	
	fmt.Println(`
Examples:
    # Pipe logs from any command (each creates its own session DB)
    docker logs myapp | logspot
    kubectl logs -f deployment/api | logspot
    tail -f /var/log/nginx/access.log | logspot

    # Search across all session databases
    logspot --search "error" --limit 20      # Search for "error" 
    logspot --search "*" --limit 10          # Show last 10 logs
    logspot --search "api" --limit 50        # Search for "api" related logs

    # Start web server for log viewing
    logspot --web                            # Start on port 8080
    logspot --web --port 9090                # Start on custom port

Architecture:
    - Each pipe operation creates one compressed Parquet file per session
    - Files stored in ~/.logspot/lake/session_*.parquet (SNAPPY compressed)
    - Efficient batched writes with immediate persistence
    - Web dashboard federates queries across all Parquet files
    - Multiple concurrent sessions supported
    - Session management and cleanup available

Lake Directory: ~/.logspot/lake/session_*.parquet`)
}

var (
	forceCliMode   = flag.Bool("cli", false, "Force CLI mode even on macOS")
	searchQuery    = flag.String("search", "", "Search logs in the database (use '*' for all logs)")
	searchLimit    = flag.Int("limit", 100, "Limit number of search results")
	showHelp       = flag.Bool("help", false, "Show usage information")
	showSessions   = flag.Bool("sessions", false, "Show active sessions")
	cleanupSessions = flag.Bool("cleanup", false, "Cleanup inactive sessions")
	webServer      = flag.Bool("web", false, "Start web server for log viewing")
	webPort        = flag.String("port", "8080", "Port for web server")
)

func main() {
	flag.Parse()
	
	// Set up the lake directory where all session DBs will be stored
	dataDir := getDataDir()
	lakeDir := filepath.Join(dataDir, "lake")
	db.SetGlobalLakeDir(lakeDir)
	
	if *showHelp {
		showUsage()
		return
	}

	if *showSessions {
		runShowSessions()
		return
	}

	if *cleanupSessions {
		runCleanupSessions()
		return
	}

	if *searchQuery != "" {
		runSearchMode(*searchQuery, *searchLimit)
		return
	}

	if *webServer {
		runWebServer(*webPort)
		return
	}

	// Check if this is a pipe operation (stdin has data)
	if isPipeOperation() {
		runPipeMode()
		return
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

