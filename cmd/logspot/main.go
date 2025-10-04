package main

import (
	"flag"
	"fmt"
	"log"
	"logspot/internal/collector"
	"logspot/internal/db"
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
	fmt.Println(`🪶 Logspot - Real-time Log Viewer with Lake Architecture

Usage:
    logspot [flags]                          # Interactive mode
    command | logspot                        # Pipe mode (creates session DB)
    logspot --search "query"                 # Search mode
    logspot --daemon                         # Daemon mode (web server)
    logspot --test                           # Generate test logs

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

    # Start daemon manually
    logspot --daemon                         # Runs web server on localhost:7777

    # Generate test data
    logspot --test                          # Creates sample logs

Architecture:
    - Each pipe operation creates its own DuckDB session file
    - All session files stored in ~/.logspot/lake/
    - Web dashboard federates queries across all sessions
    - Visit http://localhost:7777 for web interface

Lake Directory: ~/.logspot/lake/logs_session_*.duckdb`)
}

var (
	forceCliMode = flag.Bool("cli", false, "Force CLI mode even on macOS")
	searchQuery  = flag.String("search", "", "Search logs in the database (use '*' for all logs)")
	searchLimit  = flag.Int("limit", 100, "Limit number of search results")
	showHelp     = flag.Bool("help", false, "Show usage information")
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

	if *searchQuery != "" {
		runSearchMode(*searchQuery, *searchLimit)
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
	
	// Create collector manager for this session
	collectorManager := collector.NewManager()
	
	// Enable stdin collector (creates its own session DB)
	if err := collectorManager.EnableStdin("stdin"); err != nil {
		fmt.Printf("Failed to enable stdin collector: %v\n", err)
		os.Exit(1)
	}

	// Set up signal handling for manual interruption
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
	
	// Wait for either stdin to close or manual interruption
	go func() {
		// Poll the stdin collector until it's no longer running
		for {
			time.Sleep(100 * time.Millisecond)
			if stdinCollector, exists := collectorManager.GetCollector("stdin"); exists {
				if !stdinCollector.IsHealthy() {
					// Stdin collector finished, signal completion
					sigChan <- syscall.SIGPIPE
					return
				}
			} else {
				// Collector was removed, exit
				sigChan <- syscall.SIGPIPE
				return
			}
		}
	}()
	
	<-sigChan

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

// isToday checks if the given time is today
func isToday(t time.Time) bool {
	now := time.Now()
	return t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day()
}

