package collector

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"seyir/internal/config"
	"seyir/internal/db"
	"seyir/internal/parser"
	"syscall"

	"github.com/google/uuid"
)

// StdinCollector collects logs from standard input
type StdinCollector struct {
	*BaseCollector
	logParser *parser.LogParser
}

// NewStdinCollector creates a new stdin log collector
// Each collector gets its own DuckDB connection to the shared lake
func NewStdinCollector(sourceName string) *StdinCollector {
	if sourceName == "" {
		sourceName = "stdin"
	}
	
	baseCollector := NewBaseCollector(sourceName)
	if baseCollector == nil {
		return nil
	}
	
	return &StdinCollector{
		BaseCollector: baseCollector,
		logParser:     parser.NewLogParser(),
	}
}

// Start begins collecting logs from stdin
func (sc *StdinCollector) Start(ctx context.Context) error {
	// Check if stdin has data (not a TTY)
	stat, err := os.Stdin.Stat()
	if err != nil {
		return err
	}
	
	// Only proceed if stdin is not a character device (i.e., it's piped data)
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return nil // No piped data, silently return
	}
	
	sc.SetRunning(true)
	
	go func() {
		defer sc.SetRunning(false)
		
		// Set up signal handling for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGPIPE)
		
		scanner := bufio.NewScanner(os.Stdin)
		// Get max line size from config (default 1MB)
		maxScanTokenSize := config.GetCollector().MaxLineSizeBytes
		if maxScanTokenSize <= 0 {
			maxScanTokenSize = 1024 * 1024 // 1MB fallback
		}
		buf := make([]byte, maxScanTokenSize)
		scanner.Buffer(buf, maxScanTokenSize)
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-sc.StopChan():
				return
			case sig := <-sigCh:
				log.Printf("Stdin collector received signal %v, shutting down gracefully", sig)
				return
			default:
				if !scanner.Scan() {
					// Check if we hit EOF or an error
					if err := scanner.Err(); err != nil {
						log.Printf("Error reading stdin: %v", err)
					}
					// EOF reached - pipe is closed
					log.Printf("EOF reached on stdin, finishing collection")
					return
				}
				
				line := scanner.Text()
				if line == "" {
					continue
				}
				
				// Output the line to stdout (passthrough)
				fmt.Println(line)
				
				// Parse the log line using the extensible parser
				parsed := sc.logParser.Parse(line)
				
				// Convert parser.LogLevel to db.Level
				level := db.Level(parsed.Level)
				
				// Create log entry with parsed data
				entry := &db.LogEntry{
					ID:        uuid.New().String(),
					Ts:        parsed.Timestamp,
					Level:     level,
					Message:   parsed.Message,
					Source:    sc.sourceName,
					Process:   sc.sourceName,
					TraceID:   parsed.TraceID,
					Component: parsed.Service, // Use service as component
					Thread:    parsed.Thread,
					UserID:    parsed.UserID,
					RequestID: parsed.RequestID,
					Tags:      []string{},
				}
				
				sc.SaveAndBroadcast(entry)
			}
		}
	}()
	
	return nil
}

// Stop gracefully stops the stdin collector
func (sc *StdinCollector) Stop() error {
	sc.RequestStop()
	return nil
}

// Close closes any resources
func (sc *StdinCollector) Close() error {
	return nil
}

// Name returns the collector name
func (sc *StdinCollector) Name() string {
	return sc.sourceName
}

// IsHealthy returns true if the collector is functioning
func (sc *StdinCollector) IsHealthy() bool {
	return sc.IsRunning()
}

// Legacy function for backward compatibility (deprecated - DB no longer used)
func CaptureStdin(source string) {
	collector := NewStdinCollector(source)
	if collector == nil {
		log.Printf("[ERROR] Failed to create stdin collector for %s", source)
		return
	}
	ctx := context.Background()
	collector.Start(ctx)
}
