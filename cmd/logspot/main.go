package main

import (
	"log"
	"logspot/internal/core"
	"logspot/internal/modules"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

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

func main() {
	dataDir := getDataDir()
	dbPath := filepath.Join(dataDir, "logs.duckdb")
	db := core.InitDB(dbPath)

	// Pipe / stdin mode
	go modules.CaptureStdin(db, "stdin")

	// Coolify / Docker auto discovery
	if os.Getenv("ENABLE_DOCKER_CAPTURE") == "true" {
		go modules.StartDockerDiscovery(db)
	}

	// Retention
	go core.RetentionCleaner(db, 7)

	// Web server
	go startWebServer("localhost:7777")

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

	// Mac sidebar app
	if runtime.GOOS == "darwin" {
		startTray()
	}

}


func startTray() {
	systray.Run(func() {
		systray.SetTitle("Logspot")
		systray.SetTooltip("Logspot running")
		open := systray.AddMenuItem("Open Logs","Open browser")
		quit := systray.AddMenuItem("Quit","Quit Logspot")
		go func(){
			for {
				select {
				case <-open.ClickedCh: openBrowser("http://localhost:7777")
				case <-quit.ClickedCh: systray.Quit(); return
				}
			}
		}()
	}, func(){})
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

func startWebServer(addr string) {
	// Get the directory where the executable is located
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	execDir := filepath.Dir(execPath)
	webDir := filepath.Join(execDir, "web")
	
	// Fallback to current directory if web folder doesn't exist next to executable
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}
	// Simple file server for static files
	
	http.Handle("/", http.FileServer(http.Dir(webDir)))
	http.HandleFunc("/api/sse", core.SSEHandler)
	// http.HandleFunc("/api/query", 	)
	// http.HandleFunc("/api/log", apiLogHandler)
	log.Fatal(http.ListenAndServe(addr, nil))
}