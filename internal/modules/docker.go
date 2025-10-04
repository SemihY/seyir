package modules

import (
	"bufio"
	"log"
	"logspot/internal/core"
	"os/exec"
	"strings"
	"time"
)

const pollInterval = 10 * time.Second

func StartDockerDiscovery(db *core.DB) {
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
			go CaptureDockerLogs(db, c)
		}

		time.Sleep(pollInterval)
	}
}

func CaptureDockerLogs(db *core.DB, containerName string) {
	cmd := exec.Command("docker","logs","-f",containerName)
	stdout, err := cmd.StdoutPipe()
	if err != nil { log.Printf("[ERROR] %v", err); return }
	if err := cmd.Start(); err != nil { log.Printf("[ERROR] %v", err); return }

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		entry := core.NewLogEntry(containerName, core.INFO, line)
		core.SaveLog(db, entry)
		core.BroadcastLog(entry)
	}
}
