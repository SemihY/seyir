package parser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// LogLevel represents the severity of a log entry
type LogLevel string

const (
	LevelTrace   LogLevel = "TRACE"
	LevelDebug   LogLevel = "DEBUG"
	LevelInfo    LogLevel = "INFO"
	LevelWarn    LogLevel = "WARN"
	LevelWarning LogLevel = "WARNING"
	LevelError   LogLevel = "ERROR"
	LevelFatal   LogLevel = "FATAL"
	LevelPanic   LogLevel = "PANIC"
	LevelUnknown LogLevel = "UNKNOWN"
)

// ParsedLog represents a structured log entry extracted from raw text
type ParsedLog struct {
	Timestamp   time.Time
	Level       LogLevel
	Message     string
	Service     string
	Component   string
	TraceID     string
	RequestID   string
	UserID      string
	Thread      string
	Fields      map[string]string // Additional extracted fields
	RawLine     string
}

// LogPattern represents a regex pattern for parsing log formats
type LogPattern struct {
	Name        string
	Pattern     *regexp.Regexp
	TimeFormat  string
	TimeGroup   int // Which capture group contains timestamp
	LevelGroup  int // Which capture group contains level
	MsgGroup    int // Which capture group contains message
	FieldGroups map[string]int // Named field groups
}

// LogParser handles parsing various log formats
type LogParser struct {
	patterns []LogPattern
}

// NewLogParser creates a new parser with common log patterns
func NewLogParser() *LogParser {
	parser := &LogParser{
		patterns: make([]LogPattern, 0),
	}
	
	// Add default patterns
	parser.AddDefaultPatterns()
	
	return parser
}

// AddDefaultPatterns registers common log format patterns
func (p *LogParser) AddDefaultPatterns() {
	// Pattern 1: [LEVEL] [SERVICE] key=value format
	// Example: 2025-10-20T21:58:19.3NZ [INFO] [api-gateway] request_id=abc123 user=user_1234 operation=processing duration=1222ms
	p.patterns = append(p.patterns, LogPattern{
		Name:    "Structured KV with Timestamp",
		Pattern: regexp.MustCompile(`(?i)^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+\[(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)\]\s+\[([^\]]+)\]\s+(.+)$`),
		TimeFormat: "2006-01-02T15:04:05.999Z07:00",
		TimeGroup: 1,
		LevelGroup: 2,
		MsgGroup: 4,
		FieldGroups: map[string]int{
			"service": 3,
		},
	})
	
	// Pattern 2: Standard syslog-like format
	// Example: Oct 21 00:58:21 hostname service[1234]: [ERROR] Something went wrong
	p.patterns = append(p.patterns, LogPattern{
		Name:    "Syslog",
		Pattern: regexp.MustCompile(`^(\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})\s+\S+\s+(\S+)(?:\[\d+\])?\s*:\s*(?:\[(\w+)\])?\s*(.+)$`),
		TimeFormat: "Jan 2 15:04:05",
		TimeGroup: 1,
		LevelGroup: 3,
		MsgGroup: 4,
		FieldGroups: map[string]int{
			"service": 2,
		},
	})
	
	// Pattern 3: JSON-like log prefix
	// Example: {"timestamp":"2025-10-21T00:58:21Z","level":"INFO","service":"api"} Request processed
	p.patterns = append(p.patterns, LogPattern{
		Name:    "JSON Prefix",
		Pattern: regexp.MustCompile(`^\{"timestamp":"([^"]+)","level":"(\w+)"(?:,"service":"([^"]+)")?\}\s*(.*)$`),
		TimeFormat: time.RFC3339,
		TimeGroup: 1,
		LevelGroup: 2,
		MsgGroup: 4,
		FieldGroups: map[string]int{
			"service": 3,
		},
	})
	
	// Pattern 4: Simple [LEVEL] prefix
	// Example: [2025-10-21 00:58:21] [ERROR] Something bad happened
	p.patterns = append(p.patterns, LogPattern{
		Name:    "Simple Bracketed",
		Pattern: regexp.MustCompile(`^\[([^\]]+)\]\s+\[(\w+)\]\s+(.+)$`),
		TimeFormat: "2006-01-02 15:04:05",
		TimeGroup: 1,
		LevelGroup: 2,
		MsgGroup: 3,
	})
	
	// Pattern 5: Level at start without brackets
	// Example: INFO 2025-10-21 00:58:21 service: message here
	p.patterns = append(p.patterns, LogPattern{
		Name:    "Level First",
		Pattern: regexp.MustCompile(`^(\w+)\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+(\S+):\s+(.+)$`),
		TimeFormat: "2006-01-02 15:04:05",
		TimeGroup: 2,
		LevelGroup: 1,
		MsgGroup: 4,
		FieldGroups: map[string]int{
			"service": 3,
		},
	})
	
	// Pattern 6: Detect level anywhere in brackets
	// Example: 2025-10-21T00:58:21.123Z [INFO] [auth-service] User logged in
	p.patterns = append(p.patterns, LogPattern{
		Name:    "Generic Bracketed Level",
		Pattern: regexp.MustCompile(`(?i)\[(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)\]`),
		LevelGroup: 1,
	})
	
	// Pattern 7: Timestamp with brackets, level, service in parentheses, JSON message
	// Example: [20:42:47.173] INFO (carbmee-api): {"trace_id":"...","context":"...","message":"..."}
	p.patterns = append(p.patterns, LogPattern{
		Name:    "Bracketed Time Level Service JSON",
		Pattern: regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2}\.\d+)\]\s+(\w+)\s+\(([^)]+)\):\s+(.+)$`),
		TimeFormat: "15:04:05.000",
		TimeGroup: 1,
		LevelGroup: 2,
		MsgGroup: 4,
		FieldGroups: map[string]int{
			"service": 3,
		},
	})
}

// AddPattern registers a custom log pattern
func (p *LogParser) AddPattern(pattern LogPattern) {
	p.patterns = append(p.patterns, pattern)
}

// Parse attempts to parse a raw log line using registered patterns
func (p *LogParser) Parse(rawLine string) *ParsedLog {
	// Strip ANSI color codes first
	rawLine = stripANSIColors(rawLine)
	
	result := &ParsedLog{
		RawLine: rawLine,
		Level:   LevelUnknown,
		Message: rawLine, // Default to full line
		Fields:  make(map[string]string),
	}
	
	// First, try to parse as JSON
	if strings.HasPrefix(strings.TrimSpace(rawLine), "{") {
		if parsed := tryParseJSON(rawLine); parsed != nil {
			// Merge JSON parsed data into result
			if parsed.Level != LevelUnknown {
				result.Level = parsed.Level
			}
			if !parsed.Timestamp.IsZero() {
				result.Timestamp = parsed.Timestamp
			}
			if parsed.Message != "" {
				result.Message = parsed.Message
			}
			if parsed.Service != "" {
				result.Service = parsed.Service
			}
			if parsed.Component != "" {
				result.Component = parsed.Component
			}
			if parsed.TraceID != "" {
				result.TraceID = parsed.TraceID
			}
			if parsed.RequestID != "" {
				result.RequestID = parsed.RequestID
			}
			if parsed.UserID != "" {
				result.UserID = parsed.UserID
			}
			if parsed.Thread != "" {
				result.Thread = parsed.Thread
			}
			// Merge fields
			for k, v := range parsed.Fields {
				result.Fields[k] = v
			}
			
			// If JSON parsing was successful (has trace_id, component, or other identifiable fields)
			if result.Level != LevelUnknown || !result.Timestamp.IsZero() || result.TraceID != "" || result.Component != "" || len(result.Fields) > 0 {
				// Set defaults for missing fields
				if result.Level == LevelUnknown {
					result.Level = LevelInfo
				}
				if result.Timestamp.IsZero() {
					result.Timestamp = time.Now()
				}
				if result.Message == "" || result.Message == rawLine {
					// Try to create a meaningful message from fields
					if entity, ok := result.Fields["entity"]; ok {
						if operation, ok2 := result.Fields["operation"]; ok2 {
							result.Message = fmt.Sprintf("%s %s", operation, entity)
						}
					}
					if result.Message == "" || result.Message == rawLine {
						result.Message = "JSON log entry"
					}
				}
				return result
			}
		}
	}
	
	// Try each pattern in order
	for _, pattern := range p.patterns {
		matches := pattern.Pattern.FindStringSubmatch(rawLine)
		if matches == nil {
			continue
		}
		
		// Extract timestamp
		if pattern.TimeGroup > 0 && pattern.TimeGroup < len(matches) {
			if ts, err := time.Parse(pattern.TimeFormat, matches[pattern.TimeGroup]); err == nil {
				// If timestamp only has time (no date), use today's date
				if pattern.TimeFormat == "15:04:05.000" || pattern.TimeFormat == "15:04:05" {
					now := time.Now()
					result.Timestamp = time.Date(now.Year(), now.Month(), now.Day(), 
						ts.Hour(), ts.Minute(), ts.Second(), ts.Nanosecond(), ts.Location())
				} else {
					result.Timestamp = ts
				}
			}
		}
		
		// Extract log level
		if pattern.LevelGroup > 0 && pattern.LevelGroup < len(matches) {
			result.Level = normalizeLogLevel(matches[pattern.LevelGroup])
		}
		
		// Extract message
		if pattern.MsgGroup > 0 && pattern.MsgGroup < len(matches) {
			result.Message = strings.TrimSpace(matches[pattern.MsgGroup])
		}
		
		// Extract named fields
		for fieldName, groupIdx := range pattern.FieldGroups {
			if groupIdx > 0 && groupIdx < len(matches) && matches[groupIdx] != "" {
				result.Fields[fieldName] = matches[groupIdx]
				
				// Map to struct fields
				switch fieldName {
				case "service":
					result.Service = matches[groupIdx]
				case "component":
					result.Component = matches[groupIdx]
				case "trace_id", "traceid":
					result.TraceID = matches[groupIdx]
				case "request_id", "requestid":
					result.RequestID = matches[groupIdx]
				case "user_id", "userid", "user":
					result.UserID = matches[groupIdx]
				case "thread":
					result.Thread = matches[groupIdx]
				}
			}
		}
		
		// If we found a good match (has level or timestamp), stop trying patterns
		if result.Level != LevelUnknown || !result.Timestamp.IsZero() {
			break
		}
	}
	
	// Extract key-value pairs from the message
	extractKeyValuePairs(result)
	
	// If message looks like JSON, try to extract structured data from it
	if strings.HasPrefix(strings.TrimSpace(result.Message), "{") {
		if jsonParsed := tryParseJSON(result.Message); jsonParsed != nil {
			// Merge JSON fields from message into result
			if jsonParsed.TraceID != "" && result.TraceID == "" {
				result.TraceID = jsonParsed.TraceID
			}
			if jsonParsed.RequestID != "" && result.RequestID == "" {
				result.RequestID = jsonParsed.RequestID
			}
			if jsonParsed.UserID != "" && result.UserID == "" {
				result.UserID = jsonParsed.UserID
			}
			if jsonParsed.Component != "" && result.Component == "" {
				result.Component = jsonParsed.Component
			}
			// Merge all fields
			for k, v := range jsonParsed.Fields {
				if _, exists := result.Fields[k]; !exists {
					result.Fields[k] = v
				}
			}
			// Update message to be more readable
			if jsonParsed.Message != "" && jsonParsed.Message != result.Message {
				result.Message = jsonParsed.Message
			} else if entity, ok := jsonParsed.Fields["entity"]; ok {
				if operation, ok2 := jsonParsed.Fields["operation"]; ok2 {
					result.Message = fmt.Sprintf("%s %s", operation, entity)
				}
			}
		}
	}
	
	// If no timestamp was parsed, use current time
	if result.Timestamp.IsZero() {
		result.Timestamp = time.Now()
	}
	
	return result
}

// normalizeLogLevel converts various level strings to standard LogLevel
func normalizeLogLevel(level string) LogLevel {
	normalized := strings.ToUpper(strings.TrimSpace(level))
	
	switch normalized {
	case "TRACE", "TRC":
		return LevelTrace
	case "DEBUG", "DBG", "DEBG":
		return LevelDebug
	case "INFO", "INF", "INFORMATION":
		return LevelInfo
	case "WARN", "WRN", "WARNING":
		return LevelWarn
	case "ERROR", "ERR", "ERRO":
		return LevelError
	case "FATAL", "FTL", "CRIT", "CRITICAL":
		return LevelFatal
	case "PANIC", "PNC":
		return LevelPanic
	default:
		return LevelUnknown
	}
}

// extractKeyValuePairs extracts key=value pairs from the message
func extractKeyValuePairs(result *ParsedLog) {
	// Pattern: key=value (handles quoted values too)
	kvPattern := regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|[^\s,]+)`)
	matches := kvPattern.FindAllStringSubmatch(result.Message, -1)
	
	for _, match := range matches {
		if len(match) >= 3 {
			key := match[1]
			value := strings.Trim(match[2], `"`)
			
			// Store in fields map
			result.Fields[key] = value
			
			// Map to known struct fields
			switch strings.ToLower(key) {
			case "trace_id", "traceid", "trace":
				result.TraceID = value
			case "request_id", "requestid", "req_id":
				result.RequestID = value
			case "user_id", "userid", "user":
				result.UserID = value
			case "service", "svc":
				if result.Service == "" {
					result.Service = value
				}
			case "component", "comp":
				result.Component = value
			case "thread", "tid":
				result.Thread = value
			}
		}
	}
}

// ParseBatch parses multiple log lines efficiently
func (p *LogParser) ParseBatch(lines []string) []*ParsedLog {
	results := make([]*ParsedLog, len(lines))
	for i, line := range lines {
		results[i] = p.Parse(line)
	}
	return results
}

// GetSupportedFormats returns a list of supported log format descriptions
func (p *LogParser) GetSupportedFormats() []string {
	formats := make([]string, len(p.patterns))
	for i, pattern := range p.patterns {
		formats[i] = pattern.Name
	}
	return formats
}

// tryParseJSON attempts to parse a JSON-formatted log line
func tryParseJSON(rawLine string) *ParsedLog {
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(rawLine), &jsonData); err != nil {
		return nil
	}
	
	result := &ParsedLog{
		RawLine: rawLine,
		Level:   LevelUnknown,
		Fields:  make(map[string]string),
	}
	
	// Extract common JSON log fields
	for key, value := range jsonData {
		keyLower := strings.ToLower(key)
		
		// Convert value to string
		var strValue string
		switch v := value.(type) {
		case string:
			strValue = v
		case float64, int, int64:
			strValue = fmt.Sprintf("%v", v)
		case bool:
			strValue = fmt.Sprintf("%v", v)
		default:
			continue
		}
		
		// Map to known fields
		switch keyLower {
		case "level", "severity", "loglevel":
			result.Level = normalizeLogLevel(strValue)
		case "message", "msg", "text":
			result.Message = strValue
		case "timestamp", "time", "ts", "@timestamp":
			if t, err := parseJSONTimestamp(strValue); err == nil {
				result.Timestamp = t
			}
		case "service", "servicename", "svc":
			result.Service = strValue
		case "component", "comp", "logger", "context":
			result.Component = strValue
		case "trace_id", "traceid", "trace":
			result.TraceID = strValue
		case "request_id", "requestid", "req_id":
			result.RequestID = strValue
		case "user_id", "userid", "user":
			result.UserID = strValue
		case "thread", "threadid", "tid":
			result.Thread = strValue
		default:
			// Store all other fields
			result.Fields[key] = strValue
		}
	}
	
	// If no message was found, use the entire JSON as message
	if result.Message == "" {
		result.Message = rawLine
	}
	
	return result
}

// parseJSONTimestamp attempts to parse various timestamp formats
func parseJSONTimestamp(ts string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	
	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}
	
	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", ts)
}

// stripANSIColors removes ANSI color codes from log lines
func stripANSIColors(s string) string {
	// ANSI escape sequences: ESC [ ... m
	// Pattern matches: \x1b[...m or \033[...m
	ansiPattern := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiPattern.ReplaceAllString(s, "")
}
