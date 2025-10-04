package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"logspot/internal/collector"
	"logspot/internal/db"
	"net"
	"os"
	"os/exec"
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
	
	// Start the daemon if not already running
	ensureDaemonRunning()
	
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

// ensureDaemonRunning starts the daemon if it's not already running
func ensureDaemonRunning() {
	if isServerRunning("localhost:7777") {
		return // Daemon already running
	}
	
	log.Printf("[INFO] Starting daemon...")
	
	// Start daemon as background process
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("[ERROR] Failed to get executable path: %v", err)
		return
	}
	
	cmd := exec.Command(execPath, "--daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	
	if err := cmd.Start(); err != nil {
		log.Printf("[ERROR] Failed to start daemon: %v", err)
		return
	}
	
	// Wait for daemon to be ready
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if isServerRunning("localhost:7777") {
			log.Printf("[INFO] Daemon started successfully")
			return
		}
	}
	
	log.Printf("[WARN] Daemon may not have started properly")
}

// runSearchMode searches across all session databases and outputs results
func runSearchMode(query string, limit int) {
	fmt.Printf("Searching for: %s (limit: %d)\n", query, limit)
	
	lakeDir := db.GetGlobalLakeDir()
	if lakeDir == "" {
		fmt.Println("Error: Lake directory not configured")
		os.Exit(1)
	}
	
	// Find all session databases
	sessionFiles, err := filepath.Glob(filepath.Join(lakeDir, "logs_session_*.duckdb"))
	if err != nil {
		fmt.Printf("Error finding session databases: %v\n", err)
		os.Exit(1)
	}
	
	if len(sessionFiles) == 0 {
		fmt.Println("No session databases found in lake directory")
		os.Exit(0)
	}
	
	fmt.Printf("Found %d session database(s)\n", len(sessionFiles))
	
	// Create temporary DuckDB instance for federation
	tempDB, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		fmt.Printf("Error creating temporary database: %v\n", err)
		os.Exit(1)
	}
	defer tempDB.Close()
	
	// Attach all session databases for federated queries
	
	// Fallback: Attach all session databases individually
	var unionParts []string
	for i, sessionFile := range sessionFiles {
		dbAlias := fmt.Sprintf("session_%d", i)
		attachSQL := fmt.Sprintf("ATTACH '%s' AS %s", sessionFile, dbAlias)
		if _, err := tempDB.Exec(attachSQL); err != nil {
			fmt.Printf("Warning: Failed to attach %s: %v\n", sessionFile, err)
			continue
		}
		unionParts = append(unionParts, fmt.Sprintf("SELECT * FROM %s.logs", dbAlias))
		fmt.Printf("Attached: %s\n", filepath.Base(sessionFile))
	}
	
	if len(unionParts) == 0 {
		fmt.Println("No databases could be attached for querying")
		os.Exit(1)
	}
	
	// Build and execute federated query
	federatedQuery := strings.Join(unionParts, " UNION ALL ")
	if query != "" {
		federatedQuery = fmt.Sprintf("SELECT * FROM (%s) WHERE message LIKE '%%%s%%' OR source LIKE '%%%s%%'", 
			federatedQuery, query, query)
	}
	federatedQuery = fmt.Sprintf("SELECT * FROM (%s) ORDER BY ts DESC LIMIT %d", federatedQuery, limit)
	
	fmt.Printf("\nExecuting search...\n")
	rows, err := tempDB.Query(federatedQuery)
	if err != nil {
		fmt.Printf("Query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	
	// Display results
	fmt.Printf("\nResults:\n")
	fmt.Printf("%-20s %-15s %-8s %s\n", "TIMESTAMP", "SOURCE", "LEVEL", "MESSAGE")
	fmt.Printf("%s\n", strings.Repeat("-", 80))
	
	count := 0
	for rows.Next() {
		var timestamp time.Time
		var source, level, message, id string
		if err := rows.Scan(&timestamp, &source, &level, &message, &id); err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}
		
		// Format timestamp to show only time if today, otherwise show date
		timeStr := timestamp.Format("15:04:05")
		if !isToday(timestamp) {
			timeStr = timestamp.Format("2006-01-02 15:04")
		}
		
		// Truncate message if too long
		if len(message) > 50 {
			message = message[:47] + "..."
		}
		
		fmt.Printf("%-20s %-15s %-8s %s\n", timeStr, source, level, message)
		count++
	}
	
	fmt.Printf("\nFound %d matching log entries\n", count)
}

// isToday checks if the given time is today
func isToday(t time.Time) bool {
	now := time.Now()
	return t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day()
}

// isServerRunning checks if a server is already running on the given address
func isServerRunning(addr string) bool {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}


