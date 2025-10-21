package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"seyir/internal/collector"
	"seyir/internal/config"
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
    search      Search logs with filters (DuckDB-powered)
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
	
	// Load global configuration at startup (falls back to defaults)
	configPath := config.GetDefaultConfigPath(dataDir)
	if err := config.LoadConfig(configPath); err != nil {
		log.Printf("[WARN] Failed to load configuration from %s: %v", configPath, err)
		log.Printf("[INFO] Using default configuration")
	} else {
		log.Printf("[INFO] Configuration loaded from %s", configPath)
	}
	
	// Initialize batch configuration with loaded config
	if err := db.LoadDefaultConfig(); err != nil {
		log.Printf("[WARN] Failed to initialize batch configuration: %v", err)
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

	// Stop collectors and cleanup
	collectorManager.StopAll()
	
	// Cleanup ultra-light loggers
	db.CleanupUltraLightLoggers()
	
	log.Printf("[INFO] Pipe operation completed")
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
		
		// First stop collectors
		collectorManager.StopAll()
		
		// Then cleanup ultra-light loggers to export any remaining data
		db.CleanupUltraLightLoggers()
		
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
    seyir batch config set export_interval 60

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
	// Try to load configuration from default location
	config, err := db.LoadConfigFromFile(db.DefaultConfigPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	fmt.Println("Current UltraLight Configuration:")
	fmt.Printf("  Enabled: %t\n", config.UltraLight.Enabled)
	fmt.Printf("  Buffer Size: %d entries\n", config.UltraLight.BufferSize)
	fmt.Printf("  Export Interval: %d seconds\n", config.UltraLight.ExportIntervalSeconds)
	fmt.Printf("  Max Memory: %d MB\n", config.UltraLight.MaxMemoryMB)
	fmt.Printf("  Ultra Fast Mode: %t\n", config.UltraLight.UseUltraFastMode)
	
	fmt.Println("\nCompaction Configuration:")
	fmt.Printf("  Enabled: %t\n", config.Compaction.Enabled)
	fmt.Printf("  Interval: %d hours\n", config.Compaction.IntervalHours)
	fmt.Printf("  Min Files: %d\n", config.Compaction.MinFilesForCompaction)
	fmt.Printf("  Max Size: %d MB\n", config.Compaction.MaxCompactedSizeMB)
	
	fmt.Println("\nRetention Configuration:")
	fmt.Printf("  Enabled: %t\n", config.Retention.Enabled)
	fmt.Printf("  Retention Days: %d\n", config.Retention.RetentionDays)
	fmt.Printf("  Cleanup Hours: %d\n", config.Retention.CleanupHours)
	fmt.Printf("  Keep Min Files: %d\n", config.Retention.KeepMinFiles)
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
	case "buffer_size":
		if val, err := strconv.Atoi(value); err == nil && val >= 100 {
			config.UltraLight.BufferSize = val
		} else {
			fmt.Printf("Invalid buffer_size value: %s (must be >= 100)\n", value)
			return
		}
	case "export_interval":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.UltraLight.ExportIntervalSeconds = val
		} else {
			fmt.Printf("Invalid export_interval value: %s (must be >= 1)\n", value)
			return
		}
	case "max_memory_mb":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.UltraLight.MaxMemoryMB = val
		} else {
			fmt.Printf("Invalid max_memory_mb value: %s (must be >= 1)\n", value)
			return
		}
	case "ultra_fast_mode":
		if val, err := strconv.ParseBool(value); err == nil {
			config.UltraLight.UseUltraFastMode = val
		} else {
			fmt.Printf("Invalid ultra_fast_mode value: %s (must be true or false)\n", value)
			return
		}
	case "compaction_interval":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.Compaction.IntervalHours = val
		} else {
			fmt.Printf("Invalid compaction_interval value: %s (must be >= 1)\n", value)
			return
		}
	case "retention_days":
		if val, err := strconv.Atoi(value); err == nil && val >= 1 {
			config.Retention.RetentionDays = val
		} else {
			fmt.Printf("Invalid retention_days value: %s (must be >= 1)\n", value)
			return
		}
	default:
		fmt.Printf("Unknown configuration key: %s\n", key)
		fmt.Println("Available keys: buffer_size, export_interval, max_memory_mb, ultra_fast_mode, compaction_interval, retention_days")
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

// Query command functions
func runQueryUsage() {
	fmt.Println("seyir query - Fast querying with projection pushdown optimization")
	fmt.Println("")
	fmt.Println("UI Columns: ts, level, message, trace_id, source, tags")
	fmt.Println("Projection pushdown provides maximum performance by only selecting needed columns")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("    seyir query [subcommand] [options]")
	fmt.Println("")
	fmt.Println("Subcommands:")
	fmt.Println("    filter      Query with filters (exact matches only, no LIKE)")
	fmt.Println("    distinct    Get distinct values for a column")
	fmt.Println("    timerange   Show time range of all logs")
	fmt.Println("")
	fmt.Println("Available Filters (exact matches only):")
	fmt.Println("    --start         Time range start (2006-01-02 15:04:05)")
	fmt.Println("    --end           Time range end")
	fmt.Println("    --sources       Source values (comma-separated)")
	fmt.Println("    --levels        Level values (comma-separated)")
	fmt.Println("    --trace-ids     Trace ID values (comma-separated)")
	fmt.Println("    --tags          Tag values (comma-separated)")
	fmt.Println("    --limit         Result limit (default: 100)")
	fmt.Println("    --offset        Result offset")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("    # Query by time range")
	fmt.Println("    seyir query filter --start='2025-01-01 00:00:00' --end='2025-01-02 00:00:00'")
	fmt.Println("")
	fmt.Println("    # Query by exact values")
	fmt.Println("    seyir query filter --sources='app,worker' --levels='ERROR,WARN'")
	fmt.Println("")
	fmt.Println("    # Query by trace IDs")
	fmt.Println("    seyir query filter --trace-ids='abc123,def456' --limit=100")
	fmt.Println("")
	fmt.Println("    # Query with tags")
	fmt.Println("    seyir query filter --tags='urgent,critical' --levels='ERROR'")
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
	// Parse filter arguments
	filter := &db.QueryFilter{
		Limit: 100, // Default limit
	}
	
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--start":
			if i+1 < len(args) {
				if t, err := time.Parse("2006-01-02 15:04:05", args[i+1]); err == nil {
					filter.StartTime = &t
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
					filter.EndTime = &t
				} else {
					fmt.Printf("Invalid end time format: %s (use: 2006-01-02 15:04:05)\n", args[i+1])
					return
				}
				i += 2
			} else {
				fmt.Println("--end requires a value")
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
	fmt.Println("Executing fast query...")
	result, err := db.FastQuery(filter)
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
	fmt.Println(`Usage: seyir search [flags]

Flags:
  --process <name>      Filter by process name
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

  # Search by trace ID
  seyir search --trace-id abc-123-def

  # Search logs from last 2 hours
  seyir search --level WARN --since 2h

  # Search with time range
  seyir search --start 2025-10-21T00:00:00Z --end 2025-10-21T12:00:00Z
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

	// Build query
	query := db.ColumnarQuery{
		Process: *processName,
		TraceID: *traceID,
		Level:   *level,
		Source:  *source,
		Limit:   *limit,
	}

	// Parse time filters
	if *since != "" {
		duration, err := time.ParseDuration(*since)
		if err != nil {
			fmt.Printf("Invalid duration format: %v\n", err)
			os.Exit(1)
		}
		query.StartTime = time.Now().Add(-duration)
		query.EndTime = time.Now()
	}

	if *startTime != "" {
		t, err := time.Parse(time.RFC3339, *startTime)
		if err != nil {
			fmt.Printf("Invalid start time format: %v\n", err)
			os.Exit(1)
		}
		query.StartTime = t
	}

	if *endTime != "" {
		t, err := time.Parse(time.RFC3339, *endTime)
		if err != nil {
			fmt.Printf("Invalid end time format: %v\n", err)
			os.Exit(1)
		}
		query.EndTime = t
	}

	// Determine which process to search
	searchProcess := *processName
	if searchProcess == "" {
		// If no process specified, try to find the most recent one
		// For now, we'll require a process name
		fmt.Println("Error: --process flag is required")
		fmt.Println("Use 'seyir sessions' to see available processes")
		os.Exit(1)
	}

	// Perform search
	fmt.Printf("Searching logs for process '%s'...\n", searchProcess)
	results, err := db.SearchParquetWithDuckDB(searchProcess, query)
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		os.Exit(1)
	}

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
