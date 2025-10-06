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
	"strconv"
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
    seyir [flags] <command>
    command | seyir                        # Pipe mode

Commands:
    service     Start log collection service (Docker containers + Web UI)
    web         Start web server only 
    search      Search collected logs
    sessions    Show active log sessions
    cleanup     Clean up old log sessions
    batch       Manage batch buffer settings and statistics
    help        Show this help

Flags:`)
	flag.PrintDefaults()
	
	fmt.Println(`
Examples:
    # Service mode (auto-discovers containers)
    seyir --port 8080 service

    # Web interface only
    seyir --port 8080 web

    # Search logs  
    seyir --search "error" --limit 50 search
    seyir --search "trace_id=abc123" search
    seyir --search "*" --limit 10 search

    # Pipe logs directly
    docker logs mycontainer | seyir
    kubectl logs -f deployment/api | seyir

    # Management
    seyir sessions
    seyir batch stats
    seyir batch config set flush_interval 3

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
	port        = flag.String("port", "5555", "Port for web server")
	searchQuery = flag.String("search", "", "Search logs (use '*' for all logs)")
	limit       = flag.Int("limit", 100, "Search result limit")
)

func main() {
	flag.Parse()
	
	// Set up the lake directory where all session DBs will be stored
	dataDir := getDataDir()
	lakeDir := filepath.Join(dataDir, "lake")
	db.SetGlobalLakeDir(lakeDir)
	
	// Initialize batch configuration (loads from file or uses defaults)
	if err := db.LoadDefaultConfig(); err != nil {
		log.Printf("[WARN] Failed to load batch configuration: %v", err)
		log.Printf("[INFO] Using default batch configuration")
	}
	
	// Attempt to recover any emergency buffer files from previous crashes
	asyncManager := db.GetAsyncFlushManager()
	if err := asyncManager.RecoverEmergencyBuffers(); err != nil {
		log.Printf("[WARN] Failed to recover emergency buffers: %v", err)
	}
	
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
	case "batch":
		if len(args) < 2 {
			runBatchUsage()
			os.Exit(1)
		}
		runBatchCommand(args[1:])
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

	// Stop collectors and cleanup batch buffers to flush remaining data
	collectorManager.StopAll()
	db.CleanupBatchBuffers()
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
	log.Printf("[DEBUG] Received port parameter: %s", port)

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
		
		// First stop collectors
		collectorManager.StopAll()
		
		// Then cleanup batch buffers to flush any remaining data
		db.CleanupBatchBuffers()
		
		log.Printf("[INFO] ✅ Graceful shutdown completed")
	}
}

// Batch buffer management functions

func runBatchUsage() {
	fmt.Println(`seyir batch - Batch Buffer Management

Usage:
    seyir batch <command> [options]

Commands:
    config [get|set] [key] [value]    Manage batch configuration
    stats                             Show batch buffer statistics  
    flush                             Force flush all batch buffers
    retention [enable|disable|clean]  Manage log retention
    example                           Show example configuration

Examples:
    # Show current configuration
    seyir batch config get

    # Set flush interval to 3 seconds
    seyir batch config set flush_interval 3

    # Set batch size to 5000
    seyir batch config set batch_size 5000

    # Show buffer statistics
    seyir batch stats

    # Force flush all buffers
    seyir batch flush

    # Enable retention (30 days)
    seyir batch retention enable

    # Force cleanup old files  
    seyir batch retention clean

    # Show example configuration file
    seyir batch example`)
}

func runBatchCommand(args []string) {
	if len(args) == 0 {
		runBatchUsage()
		return
	}

	command := args[0]

	switch command {
	case "config":
		runBatchConfig(args[1:])
	case "stats":
		runBatchStats()
	case "flush":
		runBatchFlush()
	case "retention":
		runBatchRetention(args[1:])
	case "example":
		runBatchExample()
	default:
		fmt.Printf("Unknown batch command: %s\n", command)
		runBatchUsage()
		os.Exit(1)
	}
}

func runBatchConfig(args []string) {
	if len(args) == 0 {
		// Default to showing current config
		runBatchConfigGet()
		return
	}

	action := args[0]

	switch action {
	case "get":
		runBatchConfigGet()
	case "set":
		if len(args) < 3 {
			fmt.Println("Usage: seyir batch config set <key> <value>")
			os.Exit(1)
		}
		runBatchConfigSet(args[1], args[2])
	default:
		fmt.Printf("Unknown config action: %s\n", action)
		fmt.Println("Available actions: get, set")
		os.Exit(1)
	}
}

func runBatchConfigGet() {
	// Try to load configuration from default location
	config, err := db.LoadConfigFromFile(db.DefaultConfigPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	fmt.Println("Current Batch Buffer Configuration:")
	fmt.Printf("  Flush Interval: %d seconds\n", config.BatchBuffer.FlushIntervalSeconds)
	fmt.Printf("  Batch Size: %d logs\n", config.BatchBuffer.BatchSize)
	fmt.Printf("  Max Memory: %d MB\n", config.BatchBuffer.MaxMemoryMB)
	fmt.Printf("  Async Enabled: %t\n", config.BatchBuffer.EnableAsync)
	fmt.Printf("  Max File Size: %d MB\n", config.FileRotation.MaxFileSizeMB)
	fmt.Printf("  Max Files: %d\n", config.FileRotation.MaxFiles)
}

func runBatchConfigSet(key, value string) {
	// Load current config
	config, err := db.LoadConfigFromFile(db.DefaultConfigPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	// Update the specified key
	switch key {
	case "flush_interval":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.BatchBuffer.FlushIntervalSeconds = val
		} else {
			fmt.Printf("Invalid flush_interval value: %s (must be >= 1)\n", value)
			return
		}
	case "batch_size":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.BatchBuffer.BatchSize = val
		} else {
			fmt.Printf("Invalid batch_size value: %s (must be >= 1)\n", value)
			return
		}
	case "max_memory_mb":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.BatchBuffer.MaxMemoryMB = val
		} else {
			fmt.Printf("Invalid max_memory_mb value: %s (must be >= 1)\n", value)
			return
		}
	case "enable_async":
		if val, err := strconv.ParseBool(value); err == nil {
			config.BatchBuffer.EnableAsync = val
		} else {
			fmt.Printf("Invalid enable_async value: %s (must be true or false)\n", value)
			return
		}
	case "max_file_size_mb":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.FileRotation.MaxFileSizeMB = val
		} else {
			fmt.Printf("Invalid max_file_size_mb value: %s (must be >= 1)\n", value)
			return
		}
	case "max_files":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.FileRotation.MaxFiles = val
		} else {
			fmt.Printf("Invalid max_files value: %s (must be >= 1)\n", value)
			return
		}
	default:
		fmt.Printf("Unknown configuration key: %s\n", key)
		fmt.Println("Available keys: flush_interval, batch_size, max_memory_mb, enable_async, max_file_size_mb, max_files")
		return
	}

	// Save updated config
	if err := db.SaveConfigToFile(db.DefaultConfigPath, config); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}

	fmt.Printf("Configuration updated: %s = %s\n", key, value)
	fmt.Println("Restart seyir for changes to take effect.")
}

func runBatchStats() {
	stats := db.GetBatchBufferStats()
	
	if len(stats) == 0 {
		fmt.Println("No active batch buffers found.")
	} else {
		fmt.Printf("Batch Buffer Statistics (%d active buffers):\n", len(stats))
		fmt.Printf("%-20s %-10s %-10s %-12s %-12s %-12s %-20s\n", 
			"PROCESS", "BUF_SIZE", "CAPACITY", "TOTAL_LOGS", "FLUSHES", "ERRORS", "LAST_FLUSH")
		fmt.Printf("%s\n", "--------------------------------------------------------------------------------------------------------")

		for processName, stat := range stats {
			lastFlushStr := "Never"
			if !stat.LastFlush.IsZero() {
				lastFlushStr = stat.LastFlush.Format("15:04:05")
			}

			fmt.Printf("%-20s %-10d %-10d %-12d %-12d %-12d %-20s\n",
				processName,
				stat.BufferSize,
				stat.BufferCapacity,
				stat.TotalLogs,
				stat.TotalFlushes,
				stat.FlushErrors,
				lastFlushStr)
		}
	}
	
	// Show async flush manager stats
	fmt.Printf("\nAsync Flush Manager Statistics:\n")
	asyncManager := db.GetAsyncFlushManager()
	asyncStats := asyncManager.GetStats()
	
	fmt.Printf("  Queue: %d/%d entries\n", asyncStats.QueueLength, asyncStats.QueueCapacity)
	fmt.Printf("  Total Flushes: %d\n", asyncStats.TotalFlushes)
	fmt.Printf("  Total Errors: %d\n", asyncStats.TotalErrors)
	fmt.Printf("  Last Flush: %s\n", asyncStats.LastFlush.Format("15:04:05"))
	fmt.Printf("  Workers Active: %t\n", asyncStats.WorkersActive)
}

func runBatchFlush() {
	fmt.Println("Flushing all batch buffers...")
	
	if err := db.FlushAllBuffers(); err != nil {
		fmt.Printf("Error flushing buffers: %v\n", err)
		return
	}

	fmt.Println("All batch buffers flushed successfully.")
}

func runBatchRetention(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: seyir batch retention <enable|disable|clean|status>")
		return
	}

	command := args[0]
	retentionManager := db.GetRetentionManager()

	switch command {
	case "enable":
		config := retentionManager.GetConfig()
		config.Enabled = true
		retentionManager.UpdateConfig(config)
		fmt.Println("Retention enabled (will run background cleanup)")

	case "disable":
		config := retentionManager.GetConfig()
		config.Enabled = false
		retentionManager.UpdateConfig(config)
		fmt.Println("Retention disabled")

	case "clean":
		fmt.Println("Running manual retention cleanup...")
		retentionManager.RunCleanupNow()
		fmt.Println("Manual cleanup completed")

	case "status":
		config := retentionManager.GetConfig()
		stats := retentionManager.GetStats()
		
		fmt.Printf("Retention Status:\n")
		fmt.Printf("  Enabled: %t\n", config.Enabled)
		fmt.Printf("  Retention Days: %d\n", config.RetentionDays)
		fmt.Printf("  Cleanup Interval: %v\n", config.CleanupInterval)
		fmt.Printf("  Dry Run: %t\n", config.DryRun)
		fmt.Printf("  Keep Min Files: %d\n", config.KeepMinFiles)
		fmt.Printf("  Max Total Size: %.1f GB\n", config.MaxTotalSizeGB)
		fmt.Printf("  Last Cleanup: %s\n", stats.LastCleanup.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Files Deleted: %d\n", stats.TotalFilesDeleted)
		fmt.Printf("  Bytes Deleted: %.2f MB\n", float64(stats.TotalBytesDeleted)/(1024*1024))
		if config.Enabled && !stats.NextCleanup.IsZero() {
			fmt.Printf("  Next Cleanup: %s\n", stats.NextCleanup.Format("2006-01-02 15:04:05"))
		}

	default:
		fmt.Printf("Unknown retention command: %s\n", command)
		fmt.Println("Available commands: enable, disable, clean, status")
	}
}

func runBatchExample() {
	example := db.GetConfigExample()

	fmt.Println("Example Batch Configuration (config/config.json):")
	fmt.Println(example)
	
	fmt.Printf("\nTo use this configuration:\n")
	fmt.Printf("1. Save the above JSON to: %s\n", db.DefaultConfigPath)
	fmt.Printf("2. Restart seyir\n")
	fmt.Printf("3. Or use the HTTP API: POST /api/batch/config\n")
}

