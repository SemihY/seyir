package collector

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/signal"
	"seyir/internal/config"
	"seyir/internal/db"
	"seyir/internal/logger"
	"seyir/internal/parser"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// StdinCollector collects logs from standard input
type StdinCollector struct {
	*BaseCollector
	logParser     *parser.LogParser
	recentHashes  map[string]time.Time // Track recent log hashes to prevent duplicates
	hashMutex     sync.Mutex
	cleanupTicker *time.Ticker
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
		recentHashes:  make(map[string]time.Time),
		cleanupTicker: time.NewTicker(30 * time.Second),
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
	
	// Start cleanup goroutine for old hashes
	go func() {
		for range sc.cleanupTicker.C {
			sc.cleanupOldHashes()
		}
	}()
	
	go func() {
		defer sc.SetRunning(false)
		defer sc.cleanupTicker.Stop()
		
		// Set up signal handling for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGPIPE)
		
		scanner := bufio.NewScanner(os.Stdin)
		// Get max line size from config (default 1MB)
		cfg := config.Get()
		maxScanTokenSize := cfg.Collector.MaxLineSizeBytes
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
				logger.Debug("Stdin collector received signal %v, shutting down gracefully", sig)
				return
			default:
				if !scanner.Scan() {
					// Check if we hit EOF or an error
					if err := scanner.Err(); err != nil {
						logger.Error("Error reading stdin: %v", err)
					}
					// EOF reached - pipe is closed
					logger.Debug("EOF reached on stdin, finishing collection")
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
			
			// Use parsed service as source if available, otherwise use stdin
			source := sc.sourceName
			if parsed.Service != "" {
				source = parsed.Service
			}
			
			// Check for duplicate using hash of message + timestamp + source
			hash := sc.computeLogHash(parsed.Message, parsed.Timestamp, source)
			if sc.isDuplicate(hash) {
				continue // Skip duplicate log
			}
			
			// Create log entry with parsed data
			entry := &db.LogEntry{
				ID:        uuid.New().String(),
				Ts:        parsed.Timestamp,
				Level:     level,
				Message:   parsed.Message,
				Source:    source,
				Process:   source,
				TraceID:   parsed.TraceID,
				Component: parsed.Component, // Use component from parser
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

// computeLogHash creates a hash from message, timestamp, and source to detect duplicates
func (sc *StdinCollector) computeLogHash(message string, ts time.Time, source string) string {
	hashInput := fmt.Sprintf("%s|%d|%s", message, ts.Unix(), source)
	hash := sha256.Sum256([]byte(hashInput))
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for efficiency
}

// isDuplicate checks if this log hash was recently seen
func (sc *StdinCollector) isDuplicate(hash string) bool {
	sc.hashMutex.Lock()
	defer sc.hashMutex.Unlock()
	
	if lastSeen, exists := sc.recentHashes[hash]; exists {
		// If seen within last 5 seconds, it's a duplicate
		if time.Since(lastSeen) < 5*time.Second {
			return true
		}
	}
	
	// Mark as seen
	sc.recentHashes[hash] = time.Now()
	return false
}

// cleanupOldHashes removes hashes older than 1 minute to prevent memory growth
func (sc *StdinCollector) cleanupOldHashes() {
	sc.hashMutex.Lock()
	defer sc.hashMutex.Unlock()
	
	cutoff := time.Now().Add(-1 * time.Minute)
	for hash, lastSeen := range sc.recentHashes {
		if lastSeen.Before(cutoff) {
			delete(sc.recentHashes, hash)
		}
	}
}

// Legacy function for backward compatibility (deprecated - DB no longer used)
func CaptureStdin(source string) {
	collector := NewStdinCollector(source)
	if collector == nil {
		logger.Error("Failed to create stdin collector for %s", source)
		return
	}
	ctx := context.Background()
	collector.Start(ctx)
}
