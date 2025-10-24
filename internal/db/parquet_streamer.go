package db

import (
	"fmt"
	"os"
	"path/filepath"
	"seyir/internal/logger"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

// ParquetStreamer handles direct streaming of log entries to parquet files
type ParquetStreamer struct {
	baseDir string
	mutex   sync.Mutex
}

// NewParquetStreamer creates a new parquet streamer
func NewParquetStreamer(baseDir string) *ParquetStreamer {
	return &ParquetStreamer{
		baseDir: baseDir,
	}
}

// ParquetLogEntry represents the schema for parquet file
type ParquetLogEntry struct {
	Ts        int64  `parquet:"ts,timestamp(millisecond)"`
	Source    string `parquet:"source,snappy"`
	Level     string `parquet:"level,dict"`
	Message   string `parquet:"message,snappy"`
	ID        string `parquet:"id,optional"`
	TraceID   string `parquet:"trace_id,optional,dict"`
	Process   string `parquet:"process,optional,dict"`
	Component string `parquet:"component,optional,dict"`
	Thread    string `parquet:"thread,optional,dict"`
	UserID    string `parquet:"user_id,optional,dict"`
	RequestID string `parquet:"request_id,optional,dict"`
	Tags      string `parquet:"tags,optional,snappy"`
}

// StreamToParquet writes entries directly to a parquet file using parquet-go
func (ps *ParquetStreamer) StreamToParquet(entries []*LogEntry, processName string) (string, int64, error) {
	if len(entries) == 0 {
		return "", 0, nil
	}

	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	start := time.Now()

	// Create Hive-style partitioned directory structure
	now := time.Now()
	partitionDir := filepath.Join(
		ps.baseDir,
		processName,
		fmt.Sprintf("year=%d", now.Year()),
		fmt.Sprintf("month=%02d", now.Month()),
		fmt.Sprintf("day=%02d", now.Day()),
		fmt.Sprintf("hour=%02d", now.Hour()),
	)

	if err := os.MkdirAll(partitionDir, 0755); err != nil {
		return "", 0, fmt.Errorf("failed to create partition dir: %v", err)
	}

	// Generate unique filename with timestamp
	timestamp := now.Format("150405_000000")
	nanoSuffix := fmt.Sprintf("%d", now.UnixNano()%100000)
	fileName := fmt.Sprintf("batch_%s_%s.parquet", timestamp, nanoSuffix)
	filePath := filepath.Join(partitionDir, fileName)

	// Create parquet file
	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	// Create parquet writer with ZSTD compression
	writer := parquet.NewGenericWriter[ParquetLogEntry](
		file,
		parquet.Compression(&zstd.Codec{Level: zstd.SpeedDefault}),
		parquet.PageBufferSize(256*1024), // 256KB page buffer
	)
	defer writer.Close()

	// Convert and write entries
	parquetEntries := make([]ParquetLogEntry, len(entries))
	for i, entry := range entries {
		// Convert tags to comma-separated string
		tagsStr := ""
		if len(entry.Tags) > 0 {
			for j, tag := range entry.Tags {
				if j > 0 {
					tagsStr += ","
				}
				tagsStr += tag
			}
		}

		parquetEntries[i] = ParquetLogEntry{
			Ts:        entry.Ts.UnixMilli(),
			Source:    string(entry.Source),
			Level:     string(entry.Level),
			Message:   entry.Message,
			ID:        entry.ID,
			TraceID:   entry.TraceID,
			Process:   entry.Process,
			Component: entry.Component,
			Thread:    entry.Thread,
			UserID:    entry.UserID,
			RequestID: entry.RequestID,
			Tags:      tagsStr,
		}
	}

	// Write all entries in one batch
	_, err = writer.Write(parquetEntries)
	if err != nil {
		return "", 0, fmt.Errorf("failed to write entries: %v", err)
	}

	// Close writer to flush data
	if err := writer.Close(); err != nil {
		return "", 0, fmt.Errorf("failed to close writer: %v", err)
	}

	// Get file size
	fileInfo, err := os.Stat(filePath)
	fileSize := int64(0)
	if err == nil {
		fileSize = fileInfo.Size()
	}

	duration := time.Since(start)
	logger.Info("Streamed %d entries to %s in %v (%.2f KB)", 
		len(entries), fileName, duration, float64(fileSize)/1024)

	return filePath, fileSize, nil
}


