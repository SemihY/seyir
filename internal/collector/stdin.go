package collector

import (
	"bufio"
	"context"
	"logspot/internal/db"
	"os"
)

// StdinCollector collects logs from standard input
type StdinCollector struct {
	*BaseCollector
}

// NewStdinCollector creates a new stdin log collector
func NewStdinCollector(database *db.DB, sourceName string) *StdinCollector {
	if sourceName == "" {
		sourceName = "stdin"
	}
	
	return &StdinCollector{
		BaseCollector: NewBaseCollector(database, sourceName),
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
		
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			case <-sc.StopChan():
				return
			default:
				line := scanner.Text()
				if line == "" {
					continue
				}
				
				// Parse log level from message
				level := sc.ParseLogLevel(line)
				
				// Create and save log entry
				entry := db.NewLogEntry(sc.sourceName, level, line)
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

// Name returns the collector name
func (sc *StdinCollector) Name() string {
	return sc.sourceName
}

// IsHealthy returns true if the collector is functioning
func (sc *StdinCollector) IsHealthy() bool {
	return sc.IsRunning()
}

// Legacy function for backward compatibility
func CaptureStdin(database *db.DB, source string) {
	collector := NewStdinCollector(database, source)
	ctx := context.Background()
	collector.Start(ctx)
}
