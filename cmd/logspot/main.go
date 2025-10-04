package main

import (
	"flag"
	"fmt"
	"logspot/internal/collector"
	"logspot/internal/db"
	"logspot/internal/retention"
	"logspot/internal/server"
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
)

func main() {
	flag.Parse()

	if *testMode {
		runTestMode()
		return
	}

	// macOS'ta ve force CLI mode değilse menubar'ı çalıştır
	if runtime.GOOS == "darwin" && !*forceCliMode {
		runMenuMode()
	} else {
		runCLIMode()
	}
}

func runCLIMode() {
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "logs.duckdb")
	database := db.InitDB(dbPath)

	// Create collector manager
	collectorManager := collector.NewManager(database)
	
	// Enable stdin collector
	if err := collectorManager.EnableStdin("stdin"); err != nil {
		fmt.Printf("Failed to enable stdin collector: %v\n", err)
	}

	// Enable Docker discovery if requested
	if os.Getenv("ENABLE_DOCKER_CAPTURE") == "true" {
		if err := collectorManager.EnableDocker(); err != nil {
			fmt.Printf("Failed to enable Docker collector: %v\n", err)
		}
	}

	// Start retention cleaner
	go retention.RetentionCleaner(database, 7)

	// Start web server
	go server.StartWebServer("localhost:7777")

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
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "logs.duckdb")
	database := db.InitDB(dbPath)

	// Create collector manager
	collectorManager := collector.NewManager(database)
	
	// Enable stdin collector
	collectorManager.EnableStdin("stdin")

	// Enable Docker discovery if requested
	if os.Getenv("ENABLE_DOCKER_CAPTURE") == "true" {
		collectorManager.EnableDocker()
	}

	// Start retention cleaner
	go retention.RetentionCleaner(database, 7)

	// Start web server
	go server.StartWebServer("localhost:7777")

	// macOS menubar uygulamasını başlat
	systray.Run(onReady, func() {
		collectorManager.StopAll()
	})
}

func runTestMode() {
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "logs.duckdb")
	database := db.InitDB(dbPath)

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
		dataDir := getDataDir()
		dbPath := filepath.Join(dataDir, "logs.duckdb")
		database := db.InitDB(dbPath)
		
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



