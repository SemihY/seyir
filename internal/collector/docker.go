package collector

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"seyir/internal/db"
	"strings"
	"sync"
	"time"
)

const (
	dockerPollInterval = 10 * time.Second
	maxRetries         = 3
)

// DockerCollector manages discovery and collection of Docker container logs
type DockerCollector struct {
	*BaseCollector
	knownContainers map[string]*ContainerLogCollector
	mutex           sync.RWMutex
}

// ContainerLogCollector handles logs from a single Docker container
type ContainerLogCollector struct {
	*BaseCollector
	containerName string
	project       string
	component     string
	cmd          *exec.Cmd
}

// NewDockerCollector creates a new Docker container discovery collector
// Each collector gets its own DuckDB connection to the shared lake
func NewDockerCollector() *DockerCollector {
	baseCollector := NewBaseCollector("docker-discovery")
	if baseCollector == nil {
		return nil
	}
	
	return &DockerCollector{
		BaseCollector:   baseCollector,
		knownContainers: make(map[string]*ContainerLogCollector),
	}
}



// Start begins Docker container discovery and log collection
func (dc *DockerCollector) Start(ctx context.Context) error {
	dc.SetRunning(true)
	
	go func() {
		defer dc.SetRunning(false)
		
		ticker := time.NewTicker(dockerPollInterval)
		defer ticker.Stop()
		
		// Initial discovery
		dc.discoverContainers(ctx)
		
		for {
			select {
			case <-ctx.Done():
				dc.stopAllContainers()
				return
			case <-dc.StopChan():
				dc.stopAllContainers()
				return
			case <-ticker.C:
				dc.discoverContainers(ctx)
			}
		}
	}()
	
	return nil
}

// Stop gracefully stops all container log collection
func (dc *DockerCollector) Stop() error {
	dc.RequestStop()
	dc.stopAllContainers()
	return dc.Close()
}

// Name returns the collector name
func (dc *DockerCollector) Name() string {
	return "docker-discovery"
}

// IsHealthy returns true if Docker is accessible
func (dc *DockerCollector) IsHealthy() bool {
	if !dc.IsRunning() {
		return false
	}
	
	// Test Docker connectivity
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	return cmd.Run() == nil
}

// GetActiveContainers returns the list of currently monitored containers
func (dc *DockerCollector) GetActiveContainers() []string {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()
	
	containers := make([]string, 0, len(dc.knownContainers))
	for name := range dc.knownContainers {
		containers = append(containers, name)
	}
	return containers
}

// discoverContainers finds Docker containers that opt-in to log tracking
func (dc *DockerCollector) discoverContainers(ctx context.Context) {
	// Look for containers with seyir.enable=true label (opt-in)
	args := []string{"ps", "--filter", "label=seyir.enable=true", "--format", "{{.Names}}\t{{.Label \"seyir.project\"}}\t{{.Label \"seyir.component\"}}"}
	
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		log.Printf("[ERROR] Docker discovery failed: %v", err)
		return
	}

	currentContainers := make(map[string]bool)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	
	discoveredCount := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		
		parts := strings.Split(line, "\t")
		if len(parts) < 1 {
			continue
		}
		
		containerName := parts[0]
		project := ""
		component := ""
		
		if len(parts) > 1 {
			project = parts[1]
		}
		if len(parts) > 2 {
			component = parts[2]
		}
		
		currentContainers[containerName] = true
		discoveredCount++
		
		dc.mutex.RLock()
		_, exists := dc.knownContainers[containerName]
		dc.mutex.RUnlock()
		
		if !exists {
			dc.startContainerCollection(ctx, containerName, project, component)
		}
	}
	
	if discoveredCount > 0 {
		log.Printf("[INFO] Discovered %d containers with seyir tracking enabled", discoveredCount)
	}
	
	// Stop collection for containers that are no longer running
	dc.mutex.Lock()
	for containerName, collector := range dc.knownContainers {
		if !currentContainers[containerName] {
			collector.Stop()
			delete(dc.knownContainers, containerName)
			log.Printf("[INFO] Stopped collecting logs from container: %s", containerName)
		}
	}
	dc.mutex.Unlock()
}

// startContainerCollection begins log collection for a specific container
func (dc *DockerCollector) startContainerCollection(ctx context.Context, containerName, project, component string) {
	// Use container's project label
	containerProject := project
	
	// Create collector with project-aware naming
	sourceName := containerName
	if containerProject != "" {
		sourceName = fmt.Sprintf("%s-%s", containerProject, containerName)
	}
	
	collector := NewContainerLogCollector(sourceName)
	if collector == nil {
		log.Printf("[ERROR] Failed to create container collector for %s", containerName)
		return
	}
	
	// Set the actual container name for docker logs command
	collector.containerName = containerName
	// Set project and component for enhanced log parsing
	collector.project = containerProject
	collector.component = component
	
	dc.mutex.Lock()
	dc.knownContainers[containerName] = collector
	dc.mutex.Unlock()
	
	go func() {
		if err := collector.Start(ctx); err != nil {
			log.Printf("[ERROR] Failed to start log collection for %s: %v", containerName, err)
			
			// Remove failed container from known list
			dc.mutex.Lock()
			delete(dc.knownContainers, containerName)
			dc.mutex.Unlock()
		}
	}()
	
	log.Printf("[INFO] Started collecting logs from container: %s (project: %s, component: %s)", 
		containerName, containerProject, component)
}

// stopAllContainers stops log collection for all monitored containers
func (dc *DockerCollector) stopAllContainers() {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	
	for _, collector := range dc.knownContainers {
		collector.Stop()
	}
	dc.knownContainers = make(map[string]*ContainerLogCollector)
}

// NewContainerLogCollector creates a collector for a specific container
// Each collector gets its own DuckDB connection to the shared lake
func NewContainerLogCollector(containerName string) *ContainerLogCollector {
	baseCollector := NewBaseCollector(containerName)
	if baseCollector == nil {
		return nil
	}
	
	return &ContainerLogCollector{
		BaseCollector: baseCollector,
		containerName: containerName,
	}
}

// Start begins log collection from the Docker container
func (clc *ContainerLogCollector) Start(ctx context.Context) error {
	clc.SetRunning(true)
	
	go func() {
		defer clc.SetRunning(false)
		
		retries := 0
		for retries < maxRetries {
			if err := clc.collectLogs(ctx); err != nil {
				retries++
				log.Printf("[WARN] Container %s log collection failed (attempt %d/%d): %v", 
					clc.containerName, retries, maxRetries, err)
				
				if retries < maxRetries {
					time.Sleep(time.Duration(retries) * time.Second)
					continue
				}
				
				log.Printf("[ERROR] Failed to collect logs from %s after %d attempts", 
					clc.containerName, maxRetries)
				return
			}
			break
		}
	}()
	
	return nil
}

// collectLogs performs the actual log collection from Docker
func (clc *ContainerLogCollector) collectLogs(ctx context.Context) error {
	// Use docker logs -f to follow the container logs
	clc.cmd = exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", "10", clc.containerName)
	
	stdout, err := clc.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderr, err := clc.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	if err := clc.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker logs command: %w", err)
	}
	
	// Handle stdout
	go clc.scanLogs(stdout, "stdout")
	
	// Handle stderr
	go clc.scanLogs(stderr, "stderr")
	
	// Wait for command to finish or context to cancel
	select {
	case <-ctx.Done():
		if clc.cmd.Process != nil {
			clc.cmd.Process.Kill()
		}
		return ctx.Err()
	case <-clc.StopChan():
		if clc.cmd.Process != nil {
			clc.cmd.Process.Kill()
		}
		return nil
	}
}

// scanLogs reads and processes log lines from the given reader
func (clc *ContainerLogCollector) scanLogs(reader interface{ Read([]byte) (int, error) }, streamType string) {
	scanner := bufio.NewScanner(reader)
	
	for scanner.Scan() {
		select {
		case <-clc.StopChan():
			return
		default:
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			
			// Parse structured data from the log line
			parsedData := db.ParseLogLine(line)
			
			// Create source name with stream type if it's stderr
			sourceName := clc.containerName
			if streamType == "stderr" {
				sourceName = fmt.Sprintf("%s[stderr]", clc.containerName)
			}
			
			// Create enhanced log entry from parsed data
			var entry *db.LogEntry
			if parsedData != nil {
				entry = db.NewLogEntryFromParsed(sourceName, parsedData)
				// Add container metadata if not already set
				if entry.Process == "" {
					entry.Process = clc.containerName
				}
				if entry.Component == "" && clc.component != "" {
					entry.Component = clc.component
				}
			} else {
				// Fallback to simple parsing if structured parsing fails
				level := clc.ParseLogLevel(line)
				entry = db.NewLogEntry(sourceName, level, line)
				entry.Process = clc.containerName // Set container as process
				if clc.component != "" {
					entry.Component = clc.component
				}
			}
			
			// Always set project from container metadata if available
			if clc.project != "" {
				entry.Source = clc.project // Use project as high-level source grouping
			}
			
			clc.SaveAndBroadcast(entry)
		}
	}
}

// Stop gracefully stops the container log collection
func (clc *ContainerLogCollector) Stop() error {
	clc.RequestStop()
	
	if clc.cmd != nil && clc.cmd.Process != nil {
		clc.cmd.Process.Kill()
	}
	
	return clc.Close()
}

// Name returns the container name
func (clc *ContainerLogCollector) Name() string {
	return clc.containerName
}

// IsHealthy returns true if the container log collection is active
func (clc *ContainerLogCollector) IsHealthy() bool {
	return clc.IsRunning()
}

// Legacy functions for backward compatibility
func StartDockerDiscovery(database *db.DB) {
	collector := NewDockerCollector()
	if collector == nil {
		log.Printf("[ERROR] Failed to create Docker collector")
		return
	}
	ctx := context.Background()
	collector.Start(ctx)
}

func CaptureDockerLogs(database *db.DB, containerName string) {
	collector := NewContainerLogCollector(containerName)
	if collector == nil {
		log.Printf("[ERROR] Failed to create container collector for %s", containerName)
		return
	}
	ctx := context.Background()
	collector.Start(ctx)
}
