package db

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ANSI color code regex for cleaning log lines
var ansiColorRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type Level string

const (
	INFO Level = "INFO"
	WARN Level = "WARN"
	ERROR Level = "ERROR"
	DEBUG Level = "DEBUG"
)

type LogEntry struct {
	ID        string    `json:"id"`
	Ts        time.Time `json:"ts"`
	Source    string    `json:"source"`
	Level     Level     `json:"level"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id,omitempty"`   // Trace/correlation ID for distributed tracing
	Process   string    `json:"process,omitempty"`    // Process name/ID that generated the log
	Component string    `json:"component,omitempty"`  // Component/service name
	Thread    string    `json:"thread,omitempty"`     // Thread ID/name
	UserID    string    `json:"user_id,omitempty"`    // User ID if available
	RequestID string    `json:"request_id,omitempty"` // Request ID for HTTP requests
	Tags      []string  `json:"tags,omitempty"`       // Additional tags/labels
}

// ParsedLogData represents structured data that can be extracted from log lines
type ParsedLogData struct {
	Timestamp time.Time
	Level     Level
	Message   string
	TraceID   string
	Process   string
	Component string
	Thread    string
	UserID    string
	RequestID string
	Tags      []string
}

func NewLogEntry(source string, level Level, msg string) *LogEntry {
	return &LogEntry{
		ID:      uuid.New().String(),
		Ts:      time.Now(),
		Source:  source,
		Level:   level,
		Message: msg,
	}
}

// NewLogEntryFromParsed creates a LogEntry from parsed log data
func NewLogEntryFromParsed(source string, parsed *ParsedLogData) *LogEntry {
	entry := &LogEntry{
		ID:        uuid.New().String(),
		Source:    source,
		Level:     parsed.Level,
		Message:   parsed.Message,
		TraceID:   parsed.TraceID,
		Process:   parsed.Process,
		Component: parsed.Component,
		Thread:    parsed.Thread,
		UserID:    parsed.UserID,
		RequestID: parsed.RequestID,
		Tags:      parsed.Tags,
	}
	
	// Use parsed timestamp if available, otherwise current time
	if !parsed.Timestamp.IsZero() {
		entry.Ts = parsed.Timestamp
	} else {
		entry.Ts = time.Now()
	}
	
	return entry
}

// stripANSIColors removes ANSI color codes from log lines
func stripANSIColors(text string) string {
	return ansiColorRegex.ReplaceAllString(text, "")
}

// ParseLogLine attempts to extract structured data from a log line
func ParseLogLine(line string) *ParsedLogData {
	cleanLine := stripANSIColors(line)
	
	// Try JSON parsing first (with or without timestamp prefix)
	if jsonStart := strings.Index(cleanLine, "{"); jsonStart != -1 {
		jsonPart := cleanLine[jsonStart:]
		if jsonData := parseJSONLog(jsonPart); jsonData != nil {
			// Extract timestamp from prefix if JSON doesn't have one
			if jsonData.Timestamp.IsZero() && jsonStart > 0 {
				timestampPrefix := strings.TrimSpace(cleanLine[:jsonStart])
				if t, err := time.Parse("2006-01-02T15:04:05", timestampPrefix); err == nil {
					jsonData.Timestamp = t
				}
			}
			return jsonData
		}
	}
	
	// Fallback to plain text parsing
	return parseTextLog(cleanLine)
}

// parseTextLog handles non-JSON log formats
func parseTextLog(line string) *ParsedLogData {
	parsed := &ParsedLogData{
		Message: line,
		Level:   parseLogLevel(line),
		Tags:    []string{},
	}
	
	// Extract timestamp from common formats
	timestampRegex := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d{3})?(?:Z|[+-]\d{2}:\d{2})?)`)
	if matches := timestampRegex.FindStringSubmatch(line); len(matches) > 1 {
		if t, err := time.Parse(time.RFC3339, matches[1]); err == nil {
			parsed.Timestamp = t
		}
	}
	
	return parsed
}

// parseJSONLog attempts to parse JSON-formatted log lines
func parseJSONLog(line string) *ParsedLogData {
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(line), &jsonData); err != nil {
		return nil
	}
	
	parsed := &ParsedLogData{
		Tags: []string{},
	}
	
	// Extract core fields with priority order
	extractString := func(keys ...string) string {
		for _, key := range keys {
			if val, ok := jsonData[key].(string); ok {
				return val
			}
		}
		return ""
	}
	
	// Timestamp
	if ts := extractString("timestamp", "time", "@timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			parsed.Timestamp = t
		}
	}
	
	// Level
	if level := extractString("level", "severity"); level != "" {
		parsed.Level = parseLogLevel(level)
	}
	
	// Message
	if msg := extractString("message", "msg"); msg != "" {
		parsed.Message = msg
	} else {
		parsed.Message = line
	}
	
	// Core identifiers
	parsed.TraceID = extractString("trace_id", "traceId", "correlation_id")
	parsed.Process = extractString("app_service", "process", "service")
	parsed.Component = extractString("component", "context", "logger")
	parsed.Thread = extractString("thread", "thread_id")
	parsed.UserID = extractString("user_id", "userId")
	parsed.RequestID = extractString("request_id", "requestId")
	
	// Extract important fields as tags
	coreFields := map[string]bool{
		"timestamp": true, "time": true, "@timestamp": true,
		"level": true, "severity": true, "message": true, "msg": true,
		"trace_id": true, "traceId": true, "correlation_id": true,
		"app_service": true, "process": true, "service": true,
		"component": true, "context": true, "logger": true,
		"thread": true, "thread_id": true,
		"user_id": true, "userId": true,
		"request_id": true, "requestId": true,
	}
	
	for key, value := range jsonData {
		if !coreFields[key] {
			if strValue, ok := value.(string); ok && strValue != "" {
				parsed.Tags = append(parsed.Tags, key+":"+strValue)
			}
		}
	}
	
	return parsed
}



// parseLogLevel converts string level to Level enum
func parseLogLevel(level string) Level {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "ERROR", "ERR", "FATAL", "CRIT", "CRITICAL":
		return ERROR
	case "WARN", "WARNING":
		return WARN
	case "DEBUG", "DBG", "TRACE", "VERBOSE":
		return DEBUG
	default:
		return INFO
	}
}
