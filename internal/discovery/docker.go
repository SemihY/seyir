package discovery

import (
	"bufio"
	"log"
	"logspot/internal/db"
	"logspot/internal/tail"
	"os/exec"
	"strings"
	"time"
)

const pollInterval = 10 * time.Second

func StartDockerDiscovery(database *db.DB) {
	known := map[string]bool{}

	for {
		out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
		if err != nil {
			log.Printf("[ERROR] docker ps failed: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		containers := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, c := range containers {
			if c == "" || known[c] { continue }
			known[c] = true
			go CaptureDockerLogs(database, c)
		}

		time.Sleep(pollInterval)
	}
}

func CaptureDockerLogs(database *db.DB, containerName string) {
	cmd := exec.Command("docker","logs","-f",containerName)
	stdout, err := cmd.StdoutPipe()
	if err != nil { log.Printf("[ERROR] %v", err); return }
	if err := cmd.Start(); err != nil { log.Printf("[ERROR] %v", err); return }

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		entry := db.NewLogEntry(containerName, db.INFO, line)
		db.SaveLog(database, entry)
		tail.BroadcastLog(entry)
	}
}
