package main

import (
	"flag"
	"fmt"
	"log"
	"logspot/internal/collector"
	"logspot/internal/db"
	"logspot/internal/server"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/getlantern/systray"
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

var (
	testMode     = flag.Bool("test", false, "Generate test logs")
	forceCliMode = flag.Bool("cli", false, "Force CLI mode even on macOS")
	daemonMode   = flag.Bool("daemon", false, "Run as daemon (web server)")
)

func main() {
	flag.Parse()
	
	// Set up the lake directory where all session DBs will be stored
	dataDir := getDataDir()
	lakeDir := filepath.Join(dataDir, "lake")
	db.SetGlobalLakeDir(lakeDir)
	
	if *testMode {
		runTestMode()
		return
	}

	if *daemonMode {
		runDaemonMode()
		return
	}

	// Check if this is a pipe operation (stdin has data)
	if isPipeOperation() {
		runPipeMode()
		return
	}

	// macOS'ta ve force CLI mode değilse menubar'ı çalıştır
	if runtime.GOOS == "darwin" && !*forceCliMode {
		runMenuMode()
	} else {
		runCLIMode()
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

	// Wait for stdin to close (pipe ends)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
	<-sigChan

	// Stop collectors and exit
	collectorManager.StopAll()
	log.Printf("[INFO] Pipe operation completed")
}

// runDaemonMode runs as a persistent daemon with web server
func runDaemonMode() {
	log.Printf("[INFO] Running in daemon mode")
	
	// Start retention cleaner
	go startRetentionCleaner()

	// Start web server
	go server.StartWebServer("localhost:7777")

	// Keep daemon running
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("[INFO] Daemon shutting down")
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

func runCLIMode() {
	log.Printf("[INFO] Running in CLI mode")
	
	// Start the daemon if not already running
	ensureDaemonRunning()
	
	// Create collector manager for this session
	collectorManager := collector.NewManager()

	// Enable Docker discovery if requested
	if os.Getenv("ENABLE_DOCKER_CAPTURE") == "true" {
		if err := collectorManager.EnableDocker(); err != nil {
			fmt.Printf("Failed to enable Docker collector: %v\n", err)
		}
	}

	// Auto-open browser if not disabled
	if os.Getenv("DISABLE_AUTO_OPEN") != "true" {
		go func() {
			time.Sleep(1 * time.Second)
			openBrowser("http://localhost:7777")
		}()
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Stop all collectors
	collectorManager.StopAll()
}

func runMenuMode() {
	log.Printf("[INFO] Running in menu mode")
	
	// Start the daemon if not already running
	ensureDaemonRunning()
	
	// Create collector manager for this session
	collectorManager := collector.NewManager()

	// Enable Docker discovery if requested
	if os.Getenv("ENABLE_DOCKER_CAPTURE") == "true" {
		collectorManager.EnableDocker()
	}

	// macOS menubar uygulamasını başlat
	systray.Run(onReady, func() {
		collectorManager.StopAll()
	})
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

func runTestMode() {
	fmt.Println("Generating test logs...")
	
	// Generate test logs
	sources := []string{"app1", "app2", "nginx", "redis", "api-server"}
	levels := []string{"INFO", "ERROR", "DEBUG", "WARN"}
	messages := []string{
		"Application started successfully",
		"Connection established to database",
		"Processing HTTP request GET /api/users",
		"Cache miss for key: user:12345",
		"Task completed in 245ms",
		"Error occurred while processing request",
		"Memory usage: 87%",
		"New user registered: john@example.com",
		"Scheduled task executed",
		"Configuration reloaded",
	}

	// Create session connection for test logs
	database, err := db.NewSessionConnection()
	if err != nil {
		log.Printf("[ERROR] Failed to create session connection for test logs: %v", err)
		return
	}
	defer database.Close()

	// Generate initial test logs
	for i := 0; i < 25; i++ {
		source := sources[i%len(sources)]
		levelStr := levels[i%len(levels)]
		message := fmt.Sprintf("%s - Test entry #%d", messages[i%len(messages)], i+1)
		
		level := parseLogLevel(levelStr)
		entry := db.NewLogEntry(source, level, message)
		db.SaveLog(database, entry)
	}

	go server.StartWebServer("localhost:7777")
	
	fmt.Println("Test logs generated. Opening browser...")
	go func() {
		time.Sleep(1 * time.Second)
		openBrowser("http://localhost:7777")
	}()
	
	// Keep generating logs periodically
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		counter := 26
		
		for range ticker.C {
			source := sources[counter%len(sources)]
			levelStr := levels[counter%len(levels)]
			message := fmt.Sprintf("%s - Live test entry #%d", messages[counter%len(messages)], counter)
			
			level := parseLogLevel(levelStr)
			entry := db.NewLogEntry(source, level, message)
			db.SaveLog(database, entry)
			counter++
		}
	}()
	
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}

func parseLogLevel(levelStr string) db.Level {
	switch levelStr {
	case "ERROR":
		return db.ERROR
	case "WARN":
		return db.WARN
	case "DEBUG":
		return db.DEBUG
	default:
		return db.INFO
	}
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

// startRetentionCleaner runs retention cleanup across all session databases
func startRetentionCleaner() {
	for {
		time.Sleep(24 * time.Hour) // Run once per day
		
		lakeDir := db.GetGlobalLakeDir()
		if lakeDir == "" {
			log.Printf("[WARN] Lake directory not set, skipping retention cleanup")
			continue
		}
		
		// Find all session databases
		sessionFiles, err := filepath.Glob(filepath.Join(lakeDir, "logs_session_*.duckdb"))
		if err != nil {
			log.Printf("[ERROR] Failed to find session databases for retention: %v", err)
			continue
		}
		
		// Clean up each session database
		for _, sessionFile := range sessionFiles {
			func(dbPath string) {
				database := db.InitDB(dbPath)
				if database == nil {
					log.Printf("[ERROR] Failed to open %s for retention", dbPath)
					return
				}
				defer database.Close()
				
				// Delete logs older than 7 days
				cutoff := time.Now().AddDate(0, 0, -7)
				_, err = database.Exec(`DELETE FROM logs WHERE ts < ?`, cutoff)
				if err != nil {
					log.Printf("[ERROR] Retention cleanup failed for %s: %v", dbPath, err)
				} else {
					log.Printf("[INFO] Retention cleanup completed for %s", dbPath)
				}
			}(sessionFile)
		}
	}
}

// macOS Menubar Functions
func onReady() {
	// Skip SetIcon for now to avoid crashes
	// systray will use a default icon
	systray.SetTitle("🪶")
	systray.SetTooltip("Logspot - Real-time Log Viewer")

	mOpen := systray.AddMenuItem("🔍 Open Logs", "Open log viewer in browser")
	mTest := systray.AddMenuItem("🧪 Generate Test Logs", "Create sample log entries")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("❌ Quit", "Quit Logspot")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser("http://localhost:7777")
			case <-mTest.ClickedCh:
				generateTestLogs()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func generateTestLogs() {
	go func() {
		// Create session connection for test logs
		database, err := db.NewSessionConnection()
		if err != nil {
			log.Printf("[ERROR] Failed to create session connection for test logs: %v", err)
			return
		}
		defer database.Close()
		
		sources := []string{"menubar-test", "sample-app", "test-service"}
		levels := []string{"INFO", "WARN", "ERROR"}
		messages := []string{
			"Test log from menubar action",
			"Sample application event triggered",
			"Generated test message from menu",
		}

		for i := 0; i < 5; i++ {
			source := sources[i%len(sources)]
			levelStr := levels[i%len(levels)]
			message := fmt.Sprintf("%s #%d", messages[i%len(messages)], i+1)
			
			level := parseLogLevel(levelStr)
			entry := db.NewLogEntry(source, level, message)
			db.SaveLog(database, entry)
		}
	}()
}



