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
	// Strip ANSI color codes first
	cleanLine := stripANSIColors(line)
	
	parsed := &ParsedLogData{
		Message: cleanLine,
		Level:   INFO, // default level
		Tags:    []string{},
	}
	
	// Try to parse as JSON first
	if strings.HasPrefix(strings.TrimSpace(cleanLine), "{") {
		if jsonData := parseJSONLog(cleanLine); jsonData != nil {
			return jsonData
		}
	}
	
	// Check for JSON after timestamp prefix (e.g., "2025-10-07T11:55:19 {json}")
	if strings.Contains(cleanLine, "{") {
		// Find the first occurrence of '{'
		jsonStart := strings.Index(cleanLine, "{")
		if jsonStart != -1 {
			jsonPart := cleanLine[jsonStart:]
			if jsonData := parseJSONLog(jsonPart); jsonData != nil {
				// If we successfully parsed JSON, but timestamp is missing, try to extract from prefix
				if jsonData.Timestamp.IsZero() {
					timestampPrefix := strings.TrimSpace(cleanLine[:jsonStart])
					if t, err := time.Parse("2006-01-02T15:04:05", timestampPrefix); err == nil {
						jsonData.Timestamp = t
					}
				}
				return jsonData
			}
		}
	}
	
	// Handle SQL query logs with ANSI colors (common in database logs)
	if strings.Contains(cleanLine, "[query]") || 
	   strings.Contains(strings.ToLower(cleanLine), "insert into") || 
	   strings.Contains(strings.ToLower(cleanLine), "select ") || 
	   strings.Contains(strings.ToLower(cleanLine), "update ") {
		parseSQLLog(cleanLine, parsed)
		return parsed
	}
	
	// Parse structured text formats
	parseStructuredText(cleanLine, parsed)
	
	return parsed
}

// parseSQLLog handles SQL query logs with timing information
func parseSQLLog(line string, parsed *ParsedLogData) {
	parsed.Level = INFO
	parsed.Message = line
	parsed.Process = "database"
	
	// Initialize tags if not already done
	if parsed.Tags == nil {
		parsed.Tags = []string{}
	}
	
	// Add SQL tag
	parsed.Tags = append(parsed.Tags, "sql")
	
	// Extract timing information: [took X ms, Y results]
	timingRegex := regexp.MustCompile(`\[took (\d+) ms(?:, (\d+) results?)?\]`)
	if matches := timingRegex.FindStringSubmatch(line); len(matches) > 1 {
		parsed.Tags = append(parsed.Tags, "duration:"+matches[1]+"ms")
		if len(matches) > 2 && matches[2] != "" {
			parsed.Tags = append(parsed.Tags, "results:"+matches[2])
		}
	}
	
	// Determine query type
	lowerLine := strings.ToLower(line)
	if strings.Contains(lowerLine, "insert into") {
		parsed.Tags = append(parsed.Tags, "type:insert")
	} else if strings.Contains(lowerLine, "select ") {
		parsed.Tags = append(parsed.Tags, "type:select")
	} else if strings.Contains(lowerLine, "update ") {
		parsed.Tags = append(parsed.Tags, "type:update")
	} else if strings.Contains(lowerLine, "delete ") {
		parsed.Tags = append(parsed.Tags, "type:delete")
	}
	
	// Extract table name for INSERT/UPDATE operations
	tableRegex := regexp.MustCompile(`(?i)(?:insert into|update)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s`)
	if matches := tableRegex.FindStringSubmatch(line); len(matches) > 1 {
		parsed.Tags = append(parsed.Tags, "table:"+matches[1])
	}
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
	
	// Extract timestamp - multiple formats supported
	if ts, ok := jsonData["timestamp"].(string); ok {
		if parsedTime, err := time.Parse(time.RFC3339, ts); err == nil {
			parsed.Timestamp = parsedTime
		}
	} else if ts, ok := jsonData["time"].(string); ok {
		if parsedTime, err := time.Parse(time.RFC3339, ts); err == nil {
			parsed.Timestamp = parsedTime
		}
	} else if ts, ok := jsonData["@timestamp"].(string); ok {
		if parsedTime, err := time.Parse(time.RFC3339, ts); err == nil {
			parsed.Timestamp = parsedTime
		}
	}
	
	// Extract log level
	if level, ok := jsonData["level"].(string); ok {
		parsed.Level = parseLogLevel(level)
	} else if level, ok := jsonData["severity"].(string); ok {
		parsed.Level = parseLogLevel(level)
	}
	
	// Extract message
	if msg, ok := jsonData["message"].(string); ok {
		parsed.Message = msg
	} else if msg, ok := jsonData["msg"].(string); ok {
		parsed.Message = msg
	} else {
		// If no explicit message field, use the entire JSON as message
		parsed.Message = line
	}
	
	// Extract trace ID
	if traceID, ok := jsonData["trace_id"].(string); ok {
		parsed.TraceID = traceID
	} else if traceID, ok := jsonData["traceId"].(string); ok {
		parsed.TraceID = traceID
	} else if traceID, ok := jsonData["correlation_id"].(string); ok {
		parsed.TraceID = traceID
	}
	
	// Extract process/service - prioritize app_service 
	if appService, ok := jsonData["app_service"].(string); ok {
		parsed.Process = appService
	} else if process, ok := jsonData["process"].(string); ok {
		parsed.Process = process
	} else if service, ok := jsonData["service"].(string); ok {
		parsed.Process = service
	}
	
	// Extract component
	if component, ok := jsonData["component"].(string); ok {
		parsed.Component = component
	} else if component, ok := jsonData["logger"].(string); ok {
		parsed.Component = component
	}
	
	// Extract thread info
	if thread, ok := jsonData["thread"].(string); ok {
		parsed.Thread = thread
	} else if thread, ok := jsonData["thread_id"].(string); ok {
		parsed.Thread = thread
	}
	
	// Extract user ID
	if userID, ok := jsonData["user_id"].(string); ok {
		parsed.UserID = userID
	} else if userID, ok := jsonData["userId"].(string); ok {
		parsed.UserID = userID
	}
	
	// Extract request ID
	if requestID, ok := jsonData["request_id"].(string); ok {
		parsed.RequestID = requestID
	} else if requestID, ok := jsonData["requestId"].(string); ok {
		parsed.RequestID = requestID
	}
	
	// Extract context as tag
	if context, ok := jsonData["context"].(string); ok {
		parsed.Tags = append(parsed.Tags, "context:"+context)
	}
	
	// Extract company_id as tag
	if companyID, ok := jsonData["company_id"].(string); ok {
		parsed.Tags = append(parsed.Tags, "company:"+companyID)
	}
	
	// Extract additional fields as tags
	excludedFields := map[string]bool{
		"timestamp": true, "time": true, "@timestamp": true,
		"level": true, "severity": true,
		"message": true, "msg": true,
		"trace_id": true, "traceId": true, "correlation_id": true,
		"app_service": true, "process": true, "service": true,
		"component": true, "logger": true,
		"thread": true, "thread_id": true,
		"user_id": true, "userId": true,
		"request_id": true, "requestId": true,
		"context": true, "company_id": true,
	}
	
	// Add any other fields as tags
	for key, value := range jsonData {
		if !excludedFields[key] {
			if strValue, ok := value.(string); ok && strValue != "" {
				parsed.Tags = append(parsed.Tags, key+":"+strValue)
			}
		}
	}
	
	return parsed
}

// parseStructuredText extracts data from structured text log formats
func parseStructuredText(line string, parsed *ParsedLogData) {
	// Regex patterns for common log formats
	patterns := []struct {
		regex *regexp.Regexp
		parse func(matches []string, parsed *ParsedLogData)
	}{
		// ISO timestamp at the beginning: "2023-12-01T10:30:00Z [INFO] message"
		{
			regex: regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d{3})?(?:Z|[+-]\d{2}:\d{2})?)\s*\[?(\w+)\]?\s+(.+)$`),
			parse: func(matches []string, p *ParsedLogData) {
				if t, err := time.Parse(time.RFC3339, matches[1]); err == nil {
					p.Timestamp = t
				}
				p.Level = parseLogLevel(matches[2])
				p.Message = matches[3]
			},
		},
		// Common log format with components: "[2023-12-01 10:30:00] [INFO] [component] message"
		{
			regex: regexp.MustCompile(`^\[([^\]]+)\]\s*\[(\w+)\]\s*\[([^\]]+)\]\s+(.+)$`),
			parse: func(matches []string, p *ParsedLogData) {
				if t, err := time.Parse("2006-01-02 15:04:05", matches[1]); err == nil {
					p.Timestamp = t
				}
				p.Level = parseLogLevel(matches[2])
				p.Component = matches[3]
				p.Message = matches[4]
			},
		},
		// Extract trace IDs: "trace_id=abc123" or "traceId: def456"
		{
			regex: regexp.MustCompile(`(?i)(?:trace[_-]?id|correlation[_-]?id)[:=]\s*([a-zA-Z0-9\-]+)`),
			parse: func(matches []string, p *ParsedLogData) {
				p.TraceID = matches[1]
			},
		},
		// Extract request IDs: "request_id=abc123" or "requestId: def456"
		{
			regex: regexp.MustCompile(`(?i)(?:request[_-]?id)[:=]\s*([a-zA-Z0-9\-]+)`),
			parse: func(matches []string, p *ParsedLogData) {
				p.RequestID = matches[1]
			},
		},
		// Extract user IDs: "user_id=123" or "userId: user123"
		{
			regex: regexp.MustCompile(`(?i)(?:user[_-]?id)[:=]\s*([a-zA-Z0-9\-]+)`),
			parse: func(matches []string, p *ParsedLogData) {
				p.UserID = matches[1]
			},
		},
		// Extract process/thread info: "process=myapp" or "thread=worker-1"
		{
			regex: regexp.MustCompile(`(?i)(?:process|service)[:=]\s*([a-zA-Z0-9\-_]+)`),
			parse: func(matches []string, p *ParsedLogData) {
				p.Process = matches[1]
			},
		},
		{
			regex: regexp.MustCompile(`(?i)thread[:=]\s*([a-zA-Z0-9\-_]+)`),
			parse: func(matches []string, p *ParsedLogData) {
				p.Thread = matches[1]
			},
		},
	}
	
	// Apply all patterns to extract as much structured data as possible
	for _, pattern := range patterns {
		if matches := pattern.regex.FindStringSubmatch(line); matches != nil {
			pattern.parse(matches, parsed)
		}
	}
	
	// If no level was detected from structured parsing, use simple detection
	if parsed.Level == INFO {
		parsed.Level = parseLogLevel(line)
	}
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
