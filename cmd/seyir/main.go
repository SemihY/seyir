package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"seyir/internal/collector"
	"seyir/internal/config"
	"seyir/internal/db"
	"seyir/internal/logger"
	"seyir/internal/server"
	"seyir/internal/version"
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
	fmt.Printf("seyir %s - Centralized Log Collector & Viewer\n\n", version.Get().Short())
	fmt.Println(`Usage:
    seyir [flags] <command>
    command | seyir                        # Pipe mode

Commands:
    service     Start log collection service (Docker containers + Web UI)
    web         Start web server only 
    search      Search logs with filters (DuckDB-powered)
    session     Start and manage interactive log sessions with TUI
    batch       Manage batch buffer settings and statistics
    version     Show version information
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

    # Session management
    seyir session start myproject
    seyir session list
    seyir session cleanup

    # Batch management
    seyir batch stats
    seyir batch config set flush_interval 3

Container Setup:
    # Containers opt-in to tracking with labels:
    docker run -l seyir.enable=true my-app
    docker run -l seyir.enable=true -l seyir.project=web -l seyir.component=api backend-service

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
	showVersion = flag.Bool("version", false, "Show version information")
	showVersionShort = flag.Bool("v", false, "Show version information (short)")
)

func main() {
	flag.Parse()
	
	// Handle version flags first
	if *showVersion || *showVersionShort {
		fmt.Println(version.Get().String())
		return
	}
	
	// Set up the lake directory where all session DBs will be stored
	dataDir := getDataDir()
	lakeDir := filepath.Join(dataDir, "lake")
	db.SetGlobalLakeDir(lakeDir)
	
	// Load global configuration at startup (falls back to defaults)
	configPath := config.GetPath(dataDir)
	if err := config.Load(configPath); err != nil {
		logger.Warn("Failed to load configuration from %s: %v", configPath, err)
		logger.Info("Using default configuration")
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
		if len(args) < 2 {
			runSearchUsage()
			os.Exit(1)
		}
		runSearchCommand(args[1:])
	case "query":
		if len(args) < 2 {
			runQueryUsage()
			os.Exit(1)
		}
		runQueryCommand(args[1:])
	case "stats":
		runQueryStats()
	case "batch":
		if len(args) < 2 {
			runBatchUsage()
			os.Exit(1)
		}
		runBatchCommand(args[1:])
	case "session":
		runSessionCommand(args[1:])
	case "version":
		fmt.Println(version.Get().String())
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
	logger.Info("Running in pipe mode - creating session DB")

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
		logger.Info("Received signal, shutting down")
	case <-done:
		logger.Info("Stdin collection completed")
	}

	// Stop collectors and cleanup
	collectorManager.StopAll()
	
	// Cleanup ultra-light loggers
	db.CleanupUltraLightLoggers()
	
	logger.Info("Pipe operation completed")
}


// runSessionCleanup removes inactive sessions
func runSessionCleanup() {
	sessionManager := GetSessionManager()
	
	// Clean up sessions inactive for more than 5 minutes
	cleaned := sessionManager.CleanupInactiveSessions(5 * time.Minute)
	
	if cleaned > 0 {
		fmt.Printf("🧹 Cleaned up %d inactive sessions.\n", cleaned)
	} else {
		fmt.Println("✨ No inactive sessions to clean up.")
	}
	
	// Show remaining active sessions
	sessions := sessionManager.GetActiveSessions()
	fmt.Printf("📊 Active sessions remaining: %d\n", len(sessions))
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
	logger.Info("🚀 Starting seyir service v%s", version.GetVersion())
	logger.Info("🌐 Web interface: http://localhost:%s", port)
	logger.Info("🔍 Auto-discovering containers with 'seyir.enable=true' label")

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

	logger.Info("✅ seyir service running. Press Ctrl+C to stop.")

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrChan:
		fmt.Printf("❌ Web server failed: %v\n", err)
		collectorManager.StopAll()
		os.Exit(1)
	case <-sigChan:
		logger.Info("🛑 Shutting down seyir service...")
		
		// First stop collectors
		collectorManager.StopAll()
		
		// Then cleanup ultra-light loggers to export any remaining data
		db.CleanupUltraLightLoggers()
		
		logger.Info("✅ Graceful shutdown completed")
	}
}

// Batch buffer management functions
func runBatchUsage() {
	fmt.Println(`seyir batch - Batch Buffer Management

Usage:
    seyir batch <command> [options]

Commands:
    config [get|set] [key] [value]    Manage configuration
    stats                             Show batch buffer statistics  
    flush                             Force flush all batch buffers
    retention [enable|disable|clean]  Manage log retention
    example                           Show example configuration

Examples:
    # Show current configuration
    seyir batch config get

    # Set flush interval to 30 seconds
    seyir batch config set flush_interval 30

    # Set buffer size to 5000
    seyir batch config set buffer_size 5000

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
	cfg := config.Get()

	fmt.Printf("seyir %s\n", version.Get().Short())
	fmt.Println("Current Configuration:")
	fmt.Printf("  Buffer Size: %d entries\n", cfg.Buffer.Size)
	fmt.Printf("  Flush Interval: %d seconds\n", cfg.Buffer.FlushIntervalSeconds)
	fmt.Printf("  Max Memory: %d MB\n", cfg.Buffer.MaxMemoryMB)
	fmt.Printf("  Worker Count: %d\n", cfg.Buffer.WorkerCount)
	
	fmt.Println("\nFile Settings:")
	fmt.Printf("  Max File Size: %d MB\n", cfg.Files.MaxSizeMB)
	fmt.Printf("  Max Files: %d\n", cfg.Files.MaxFiles)
	fmt.Printf("  Compression: %s\n", cfg.Files.Compression)
	
	fmt.Println("\nRetention:")
	fmt.Printf("  Enabled: %t\n", cfg.Retention.Enabled)
	fmt.Printf("  Days: %d\n", cfg.Retention.Days)
	fmt.Printf("  Cleanup Hours: %d\n", cfg.Retention.CleanupHours)
	fmt.Printf("  Keep Min Files: %d\n", cfg.Retention.KeepMinFiles)
	fmt.Printf("  Max Total Size: %.1f GB\n", cfg.Retention.MaxTotalSizeGB)
	
	fmt.Println("\nServer:")
	fmt.Printf("  Port: %s\n", cfg.Server.Port)
	fmt.Printf("  CORS Enabled: %t\n", cfg.Server.EnableCORS)
	
	fmt.Println("\nDebug:")
	fmt.Printf("  Enabled: %t\n", cfg.Debug.Enabled)
}

func runBatchConfigSet(key, value string) {
	// Set configuration value
	if err := config.Set(key, value); err != nil {
		fmt.Printf("Error setting config: %v\n", err)
		return
	}

	// Save configuration
	dataDir := getDataDir()
	configPath := config.GetPath(dataDir)
	if err := config.Save(configPath); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}

	fmt.Printf("Configuration updated: %s = %s\n", key, value)
	fmt.Println("Restart seyir for changes to take effect.")
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
	example := config.Default()
	data, _ := json.MarshalIndent(example, "", "  ")
	
	fmt.Println("Example Configuration (config.json):")
	fmt.Println(string(data))
	
	fmt.Printf("\nTo use this configuration:\n")
	dataDir := getDataDir()
	configPath := config.GetPath(dataDir)
	fmt.Printf("1. Save the above JSON to: %s\n", configPath)
	fmt.Printf("2. Restart seyir\n")
	fmt.Printf("3. Or use: seyir batch config set <key> <value>\n")
}

// Query command functions
func runQueryUsage() {
	fmt.Println("seyir query - Unified querying with folder structure optimization")
	fmt.Println("")
	fmt.Println("UI Columns: ts, level, message, trace_id, source, tags, process")
	fmt.Println("Supports full-text search and folder-level optimization")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("    seyir query [subcommand] [options]")
	fmt.Println("")
	fmt.Println("Subcommands:")
	fmt.Println("    filter      Query with filters (includes full-text search)")
	fmt.Println("    distinct    Get distinct values for a column")
	fmt.Println("    timerange   Show time range of all logs")
	fmt.Println("")
	fmt.Println("Available Filters:")
	fmt.Println("    --start         Time range start (2006-01-02 15:04:05)")
	fmt.Println("    --end           Time range end")
	fmt.Println("    --process       Process name (enables folder optimization)")
	fmt.Println("    --search        Message text search (supports full-text)")
	fmt.Println("    --sources       Source values (comma-separated)")
	fmt.Println("    --levels        Level values (comma-separated)")
	fmt.Println("    --trace-ids     Trace ID values (comma-separated)")
	fmt.Println("    --tags          Tag values (comma-separated)")
	fmt.Println("    --limit         Result limit (default: 100)")
	fmt.Println("    --offset        Result offset")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("    # Query by time range (defaults to last 24h)")
	fmt.Println("    seyir query filter --start='2025-01-01 00:00:00' --end='2025-01-02 00:00:00'")
	fmt.Println("")
	fmt.Println("    # Full-text search with process filter")
	fmt.Println("    seyir query filter --process=myapp --search='database error' --levels='ERROR'")
	fmt.Println("")
	fmt.Println("    # Query by trace IDs")
	fmt.Println("    seyir query filter --trace-ids='abc123,def456' --limit=100")
	fmt.Println("")
	fmt.Println("    # Search across all processes")
	fmt.Println("    seyir query filter --search='timeout' --levels='ERROR,WARN'")
	fmt.Println("")
	fmt.Println("    # Get distinct sources")
	fmt.Println("    seyir query distinct --column=source --limit=50")
	fmt.Println("")
	fmt.Println("    # Show time range")
	fmt.Println("    seyir query timerange")
}

func runQueryCommand(args []string) {
	if len(args) == 0 {
		runQueryUsage()
		return
	}
	
	subcommand := args[0]
	
	switch subcommand {
	case "filter":
		runQueryFilter(args[1:])
	case "distinct":
		runQueryDistinct(args[1:])
	case "timerange":
		runQueryTimeRange()
	default:
		fmt.Printf("Unknown query subcommand: %s\n", subcommand)
		runQueryUsage()
	}
}

func runQueryFilter(args []string) {
	// Parse filter arguments - default to last 24 hours if no time range specified
	now := time.Now()
	filter := &db.QueryFilter{
		StartTime: now.Add(-24 * time.Hour), // Default: last 24 hours
		EndTime:   now,
		Limit:     100, // Default limit
	}
	
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--start":
			if i+1 < len(args) {
				if t, err := time.Parse("2006-01-02 15:04:05", args[i+1]); err == nil {
					filter.StartTime = t
				} else {
					fmt.Printf("Invalid start time format: %s (use: 2006-01-02 15:04:05)\n", args[i+1])
					return
				}
				i += 2
			} else {
				fmt.Println("--start requires a value")
				return
			}
		case "--end":
			if i+1 < len(args) {
				if t, err := time.Parse("2006-01-02 15:04:05", args[i+1]); err == nil {
					filter.EndTime = t
				} else {
					fmt.Printf("Invalid end time format: %s (use: 2006-01-02 15:04:05)\n", args[i+1])
					return
				}
				i += 2
			} else {
				fmt.Println("--end requires a value")
				return
			}
		case "--process":
			if i+1 < len(args) {
				filter.ProcessName = args[i+1]
				i += 2
			} else {
				fmt.Println("--process requires a value")
				return
			}
		case "--search":
			if i+1 < len(args) {
				filter.MessageSearch = args[i+1]
				i += 2
			} else {
				fmt.Println("--search requires a value")
				return
			}
		case "--sources":
			if i+1 < len(args) {
				filter.Sources = strings.Split(args[i+1], ",")
				i += 2
			} else {
				fmt.Println("--sources requires a value")
				return
			}
		case "--levels":
			if i+1 < len(args) {
				filter.Levels = strings.Split(args[i+1], ",")
				i += 2
			} else {
				fmt.Println("--levels requires a value")
				return
			}
		case "--trace-ids":
			if i+1 < len(args) {
				filter.TraceIDs = strings.Split(args[i+1], ",")
				i += 2
			} else {
				fmt.Println("--trace-ids requires a value")
				return
			}
		case "--tags":
			if i+1 < len(args) {
				filter.Tags = strings.Split(args[i+1], ",")
				i += 2
			} else {
				fmt.Println("--tags requires a value")
				return
			}
		case "--limit":
			if i+1 < len(args) {
				if limit, err := strconv.Atoi(args[i+1]); err == nil {
					filter.Limit = limit
				} else {
					fmt.Printf("Invalid limit value: %s\n", args[i+1])
					return
				}
				i += 2
			} else {
				fmt.Println("--limit requires a value")
				return
			}
		case "--offset":
			if i+1 < len(args) {
				if offset, err := strconv.Atoi(args[i+1]); err == nil {
					filter.Offset = offset
				} else {
					fmt.Printf("Invalid offset value: %s\n", args[i+1])
					return
				}
				i += 2
			} else {
				fmt.Println("--offset requires a value")
				return
			}
		default:
			fmt.Printf("Unknown filter option: %s\n", args[i])
			return
		}
	}
	
	// Initialize lake directory
	dataDir := getDataDir()
	db.SetGlobalLakeDir(filepath.Join(dataDir, "lake"))
	
	// Execute query
	fmt.Println("Executing query...")
	result, err := db.Query(filter)
	if err != nil {
		fmt.Printf("Query failed: %v\n", err)
		return
	}
	
	// Display results
	fmt.Printf("\nQuery Results:\n")
	fmt.Printf("Files scanned: %d\n", result.FilesScanned)
	fmt.Printf("Query time: %v\n", result.QueryTime)
	fmt.Printf("Total matches: %d\n", result.TotalCount)
	fmt.Printf("Showing: %d entries\n\n", len(result.Entries))
	
	if len(result.Entries) > 0 {
		fmt.Printf("%-20s %-8s %-15s %-12s %s\n", "TIMESTAMP", "LEVEL", "SOURCE", "TRACE_ID", "MESSAGE")
		fmt.Printf("%s\n", strings.Repeat("-", 100))
		
		for _, entry := range result.Entries {
			traceID := entry.TraceID
			if traceID == "" {
				traceID = "-"
			}
			if len(traceID) > 12 {
				traceID = traceID[:9] + "..."
			}
			
			message := entry.Message
			if len(message) > 50 {
				message = message[:47] + "..."
			}
			
			// Show tags if available
			tagsStr := ""
			if len(entry.Tags) > 0 {
				tagsStr = fmt.Sprintf(" [%s]", strings.Join(entry.Tags, ","))
			}
			
			fmt.Printf("%-20s %-8s %-15s %-12s %s%s\n", 
				entry.Ts.Format("2006-01-02 15:04:05"),
				entry.Level, entry.Source, traceID, message, tagsStr)
		}
	}
}

func runQueryDistinct(args []string) {
	var column string
	var limit = 100
	
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--column":
			if i+1 < len(args) {
				column = args[i+1]
				i += 2
			} else {
				fmt.Println("--column requires a value")
				return
			}
		case "--limit":
			if i+1 < len(args) {
				if l, err := strconv.Atoi(args[i+1]); err == nil {
					limit = l
				} else {
					fmt.Printf("Invalid limit value: %s\n", args[i+1])
					return
				}
				i += 2
			} else {
				fmt.Println("--limit requires a value")
				return
			}
		default:
			fmt.Printf("Unknown option: %s\n", args[i])
			return
		}
	}
	
	if column == "" {
		fmt.Println("--column is required")
		fmt.Println("Valid columns: source, level, process, component, thread, user_id, request_id, trace_id")
		return
	}
	
	// Initialize lake directory
	dataDir := getDataDir()
	db.SetGlobalLakeDir(filepath.Join(dataDir, "lake"))
	
	// Get distinct values
	values, err := db.GetDistinctValues(column, limit)
	if err != nil {
		fmt.Printf("Failed to get distinct values: %v\n", err)
		return
	}
	
	fmt.Printf("Distinct values for column '%s' (limit %d):\n\n", column, limit)
	for i, value := range values {
		fmt.Printf("%d. %s\n", i+1, value)
	}
	fmt.Printf("\nTotal: %d values\n", len(values))
}

func runQueryTimeRange() {
	// Initialize lake directory
	dataDir := getDataDir()
	db.SetGlobalLakeDir(filepath.Join(dataDir, "lake"))
	
	// Get time range
	minTime, maxTime, err := db.GetTimeRange()
	if err != nil {
		fmt.Printf("Failed to get time range: %v\n", err)
		return
	}
	
	if minTime == nil || maxTime == nil {
		fmt.Println("No log data found")
		return
	}
	
	fmt.Printf("Log Time Range:\n")
	fmt.Printf("Oldest: %s\n", minTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("Newest: %s\n", maxTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("Duration: %v\n", maxTime.Sub(*minTime))
}

func runQueryStats() {
	// Initialize lake directory
	dataDir := getDataDir()
	db.SetGlobalLakeDir(filepath.Join(dataDir, "lake"))
	
	// Get statistics
	fmt.Println("Collecting query statistics...")
	stats, err := db.GetQueryStats()
	if err != nil {
		fmt.Printf("Failed to get statistics: %v\n", err)
		return
	}
	
	fmt.Printf("\nQuery Statistics:\n")
	fmt.Printf("Total Files: %d\n", stats.TotalFiles)
	fmt.Printf("Total Size: %.2f MB\n", float64(stats.TotalSizeBytes)/(1024*1024))
	fmt.Printf("Total Records: %d\n", stats.TotalRecords)
	if stats.OldestTimestamp != nil {
		fmt.Printf("Oldest Log: %s\n", stats.OldestTimestamp.Format("2006-01-02 15:04:05"))
	}
	if stats.NewestTimestamp != nil {
		fmt.Printf("Newest Log: %s\n", stats.NewestTimestamp.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("Unique Processes: %d\n", stats.UniqueProcesses)
	fmt.Printf("Unique Sources: %d\n", stats.UniqueSources)
	fmt.Printf("Unique Levels: %d\n", stats.UniqueLevels)
	fmt.Printf("Query Time: %v\n", stats.QueryTime)
}

// runSearchUsage shows search command usage
func runSearchUsage() {
	fmt.Print(`Usage: seyir search [flags]

Flags:
  --process <name>      Filter by process name (optional, enables optimization)
  --trace-id <id>       Filter by trace ID
  --level <level>       Filter by log level (INFO, WARN, ERROR, DEBUG)
  --source <source>     Filter by source
  --since <duration>    Filter by time (e.g., 1h, 30m, 2h30m)
  --start <time>        Start time (RFC3339 format)
  --end <time>          End time (RFC3339 format)
  --limit <n>           Maximum number of results (default: 1000)

Examples:
  # Search for errors in a specific process
  seyir search --process myapp --level ERROR --limit 100

  # Search by trace ID across all processes
  seyir search --trace-id abc-123-def

  # Search logs from last 2 hours
  seyir search --level WARN --since 2h

  # Search with time range across all processes
  seyir search --start 2025-10-21T00:00:00Z --end 2025-10-21T12:00:00Z --level ERROR
`)
}

// runSearchCommand performs a DuckDB-powered search on parquet files
func runSearchCommand(args []string) {
	// Parse flags
	searchFlags := flag.NewFlagSet("search", flag.ExitOnError)
	processName := searchFlags.String("process", "", "Process name")
	traceID := searchFlags.String("trace-id", "", "Trace ID")
	level := searchFlags.String("level", "", "Log level")
	source := searchFlags.String("source", "", "Source")
	since := searchFlags.String("since", "", "Time duration (e.g., 1h, 30m)")
	startTime := searchFlags.String("start", "", "Start time (RFC3339)")
	endTime := searchFlags.String("end", "", "End time (RFC3339)")
	limit := searchFlags.Int("limit", 1000, "Result limit")

	searchFlags.Parse(args)

	// Initialize lake directory
	dataDir := getDataDir()
	db.SetGlobalLakeDir(filepath.Join(dataDir, "lake"))

	// Build query filter - default to today if no time specified
	now := time.Now()
	// Start from beginning of today
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	filter := &db.QueryFilter{
		StartTime: startOfToday, // Default: from start of today
		EndTime:   now,
		Limit:     *limit,
	}

	// Set process filter
	if *processName != "" {
		filter.ProcessName = *processName
	}
	
	// Set other filters
	if *traceID != "" {
		filter.TraceIDs = []string{*traceID}
	}
	if *level != "" {
		filter.Levels = []string{*level}
	}
	if *source != "" {
		filter.Sources = []string{*source}
	}

	// Parse time filters
	if *since != "" {
		duration, err := time.ParseDuration(*since)
		if err != nil {
			fmt.Printf("Invalid duration format: %v\n", err)
			os.Exit(1)
		}
		filter.StartTime = time.Now().Add(-duration)
		filter.EndTime = time.Now()
	}

	if *startTime != "" {
		t, err := time.Parse(time.RFC3339, *startTime)
		if err != nil {
			fmt.Printf("Invalid start time format: %v\n", err)
			os.Exit(1)
		}
		filter.StartTime = t
	}

	if *endTime != "" {
		t, err := time.Parse(time.RFC3339, *endTime)
		if err != nil {
			fmt.Printf("Invalid end time format: %v\n", err)
			os.Exit(1)
		}
		filter.EndTime = t
	}

	// Perform search
	if filter.ProcessName != "" {
		fmt.Printf("Searching logs for process '%s'...\n", filter.ProcessName)
	} else {
		fmt.Println("Searching logs across all processes...")
	}
	
	result, err := db.Query(filter)
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		os.Exit(1)
	}
	
	results := result.Entries

	// Display results
	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}

	fmt.Printf("\nFound %d results:\n", len(results))
	fmt.Println(strings.Repeat("-", 120))

	for _, entry := range results {
		timestamp := entry.Ts.Format("2006-01-02 15:04:05")
		traceInfo := ""
		if entry.TraceID != "" {
			traceInfo = fmt.Sprintf(" [trace:%s]", entry.TraceID)
		}

		fmt.Printf("[%s] [%s] %s%s\n", timestamp, entry.Level, entry.Message, traceInfo)
		
		// Show additional fields if present
		if entry.Component != "" || entry.Thread != "" {
			details := []string{}
			if entry.Component != "" {
				details = append(details, fmt.Sprintf("component=%s", entry.Component))
			}
			if entry.Thread != "" {
				details = append(details, fmt.Sprintf("thread=%s", entry.Thread))
			}
			fmt.Printf("  └─ %s\n", strings.Join(details, ", "))
		}
	}

	fmt.Println(strings.Repeat("-", 120))
	fmt.Printf("Total: %d entries\n", len(results))
}

// Session command functions
func runSessionUsage() {
	fmt.Println(`seyir session - Interactive log sessions with TUI

Usage:
    seyir session <command> [options]

Commands:
    start <session-name>              Start a new session with TUI interface
    attach <session-name> <process>   Attach a process to existing session (use with pipe)
    list                              List active sessions
    stop <session-name>               Stop a session
    cleanup                           Clean up inactive sessions

Examples:
    # Start a new session (opens TUI)
    seyir session start myproject

    # In another terminal, attach processes
    docker-compose logs -f api | seyir session attach myproject api
    docker-compose logs -f db  | seyir session attach myproject db
    
    # List active sessions
    seyir session list
    
    # Stop a session
    seyir session stop myproject
    
    # Clean up inactive sessions
    seyir session cleanup

Session Features:
    - Multiple processes per session
    - Real-time TUI with filtering
    - Auto-refresh every 3 seconds
    - View modes: all logs, errors, warnings, traces
    - Persistent session state`)
}

func runSessionCommand(args []string) {
	if len(args) == 0 {
		runSessionUsage()
		return
	}

	command := args[0]

	switch command {
	case "start":
		runSessionStart(args[1:])
	case "attach":
		runSessionAttach(args[1:])
	case "list":
		runSessionList()
	case "stop":
		runSessionStop(args[1:])
	case "cleanup":
		runSessionCleanup()
	default:
		fmt.Printf("Unknown session command: %s\n", command)
		runSessionUsage()
		os.Exit(1)
	}
}

func runSessionStart(args []string) {
	sessionName := "default"
	if len(args) > 0 {
		sessionName = args[0]
	}

	// Initialize lake directory for querying
	dataDir := getDataDir()
	db.SetGlobalLakeDir(filepath.Join(dataDir, "lake"))

	// Create or get existing session
	sessionManager := GetSessionManager()
	session := sessionManager.CreateSession(sessionName)

	fmt.Printf("🚀 Started session: %s (ID: %s)\n", sessionName, session.ID)
	fmt.Printf("📎 Attach processes with:\n")
	fmt.Printf("   docker logs -f <container> | seyir session attach %s <process-name>\n", sessionName)
	fmt.Printf("   kubectl logs -f pod/app | seyir session attach %s app\n", sessionName)
	fmt.Printf("\n⚡ Starting TUI interface...\n\n")

	// Start TUI
	if err := StartTUI(session); err != nil {
		fmt.Printf("❌ TUI failed: %v\n", err)
		os.Exit(1)
	}
}

func runSessionAttach(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: seyir session attach <session-name> <process-name>")
		fmt.Println("Example: docker logs -f api | seyir session attach myproject api")
		os.Exit(1)
	}

	sessionName := args[0]
	processName := args[1]

	// Get or create session
	sessionManager := GetSessionManager()
	session, exists := sessionManager.GetSessionByName(sessionName)
	if !exists {
		// Create new session if it doesn't exist
		session = sessionManager.CreateSession(sessionName)
		fmt.Printf("📝 Created new session: %s\n", sessionName)
	}

	// Attach process to session
	session.AttachProcess(processName)
	fmt.Printf("📎 Attached process '%s' to session '%s'\n", processName, sessionName)
	fmt.Printf("💡 View logs with: seyir session start %s\n", sessionName)

	// Use the same collector infrastructure as pipe mode
	logger.Info("Starting session attach with collector infrastructure")

	collectorManager := collector.NewManager()

	// Enable stdin collector with the process name as source
	if err := collectorManager.EnableStdin(processName); err != nil {
		fmt.Printf("❌ Failed to enable stdin collector: %v\n", err)
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
		logger.Info("Received signal, shutting down session attach")
	case <-done:
		logger.Info("Session attach stdin collection completed")
	}

	// Stop collectors and cleanup
	collectorManager.StopAll()
	
	// Cleanup ultra-light loggers
	db.CleanupUltraLightLoggers()
	
	fmt.Printf("✅ Finished processing logs for process '%s' in session '%s'\n", processName, sessionName)
}

func runSessionList() {
	sessionManager := GetSessionManager()
	sessions := sessionManager.GetActiveSessions()

	if len(sessions) == 0 {
		fmt.Println("No active sessions found.")
		return
	}

	fmt.Printf("Active Sessions (%d):\n\n", len(sessions))
	fmt.Printf("%-20s %-12s %-10s %-8s %-10s %s\n", 
		"NAME", "ID", "DURATION", "PROCESSES", "LOGS", "LAST UPDATE")
	fmt.Printf("%s\n", strings.Repeat("-", 80))

	for _, session := range sessions {
		stats := session.GetStats()
		duration := formatDuration(stats.Duration)
		lastUpdate := session.LastUpdate.Format("15:04:05")

		fmt.Printf("%-20s %-12s %-10s %-8d %-10d %s\n",
			session.Name,
			session.ID[:8]+"...",
			duration,
			stats.ProcessCount,
			stats.TotalLogs,
			lastUpdate)
	}
	
	fmt.Printf("\nUse 'seyir session start <name>' to view a session's logs in TUI.\n")
}

func runSessionStop(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: seyir session stop <session-name>")
		os.Exit(1)
	}

	sessionName := args[0]
	sessionManager := GetSessionManager()
	
	session, exists := sessionManager.GetSessionByName(sessionName)
	if !exists {
		fmt.Printf("❌ Session '%s' not found\n", sessionName)
		os.Exit(1)
	}

	session.Stop()
	fmt.Printf("🛑 Stopped session: %s\n", sessionName)
}


