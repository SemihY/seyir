package parser

import (
	"testing"
)

func TestLogParser_Parse(t *testing.T) {
	parser := NewLogParser()
	
	tests := []struct {
		name          string
		input         string
		expectedLevel LogLevel
		expectedService string
		expectedFields map[string]string
	}{
		{
			name:          "Structured KV with Timestamp",
			input:         "2025-10-20T21:58:19.300Z [ERROR] [api-gateway] request_id=abc123 user=user_1234 operation=processing duration=1222ms",
			expectedLevel: LevelError,
			expectedService: "api-gateway",
			expectedFields: map[string]string{
				"request_id": "abc123",
				"user": "user_1234",
				"operation": "processing",
				"duration": "1222ms",
			},
		},
		{
			name:          "Simple Bracketed Level",
			input:         "[2025-10-21 00:58:21] [WARN] Database connection slow",
			expectedLevel: LevelWarn,
		},
		{
			name:          "Level at start",
			input:         "INFO 2025-10-21 00:58:21 auth-service: User authentication successful",
			expectedLevel: LevelInfo,
			expectedService: "auth-service",
		},
		{
			name:          "JSON Prefix",
			input:         `{"timestamp":"2025-10-21T00:58:21Z","level":"DEBUG","service":"payment"} Processing payment`,
			expectedLevel: LevelDebug,
			expectedService: "payment",
		},
		{
			name:          "Mixed case level",
			input:         "2025-10-21T00:58:21Z [warning] [user-service] Cache miss detected",
			expectedLevel: LevelWarn,
			expectedService: "user-service",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input)
			
			if result.Level != tt.expectedLevel {
				t.Errorf("Expected level %s, got %s", tt.expectedLevel, result.Level)
			}
			
			if tt.expectedService != "" && result.Service != tt.expectedService {
				t.Errorf("Expected service %s, got %s", tt.expectedService, result.Service)
			}
			
			for key, expectedValue := range tt.expectedFields {
				if actualValue, ok := result.Fields[key]; !ok {
					t.Errorf("Expected field %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Field %s: expected %s, got %s", key, expectedValue, actualValue)
				}
			}
			
			if result.Timestamp.IsZero() {
				t.Error("Timestamp should not be zero")
			}
		})
	}
}

func TestNormalizeLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"INFO", LevelInfo},
		{"info", LevelInfo},
		{"InFo", LevelInfo},
		{"ERROR", LevelError},
		{"err", LevelError},
		{"WARN", LevelWarn},
		{"WARNING", LevelWarn},
		{"DEBUG", LevelDebug},
		{"dbg", LevelDebug},
		{"FATAL", LevelFatal},
		{"TRACE", LevelTrace},
		{"PANIC", LevelPanic},
		{"UNKNOWN_LEVEL", LevelUnknown},
	}
	
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLogLevel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLogLevel(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractKeyValuePairs(t *testing.T) {
	result := &ParsedLog{
		Message: `request_id=abc123 user=user_1234 status="in progress" count=42`,
		Fields:  make(map[string]string),
	}
	
	extractKeyValuePairs(result)
	
	expected := map[string]string{
		"request_id": "abc123",
		"user": "user_1234",
		"status": "in progress",
		"count": "42",
	}
	
	for key, expectedValue := range expected {
		if actualValue, ok := result.Fields[key]; !ok {
			t.Errorf("Expected field %s not found", key)
		} else if actualValue != expectedValue {
			t.Errorf("Field %s: expected %s, got %s", key, expectedValue, actualValue)
		}
	}
	
	// Check that special fields were mapped to struct fields
	if result.RequestID != "abc123" {
		t.Errorf("RequestID should be abc123, got %s", result.RequestID)
	}
	if result.UserID != "user_1234" {
		t.Errorf("UserID should be user_1234, got %s", result.UserID)
	}
}

func TestParseBatch(t *testing.T) {
	parser := NewLogParser()
	
	lines := []string{
		"2025-10-21T00:58:21Z [INFO] [service1] Starting service",
		"2025-10-21T00:58:22Z [ERROR] [service2] Failed to connect",
		"2025-10-21T00:58:23Z [WARN] [service3] Slow query detected",
	}
	
	results := parser.ParseBatch(lines)
	
	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}
	
	expectedLevels := []LogLevel{LevelInfo, LevelError, LevelWarn}
	for i, result := range results {
		if result.Level != expectedLevels[i] {
			t.Errorf("Result %d: expected level %s, got %s", i, expectedLevels[i], result.Level)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	parser := NewLogParser()
	line := "2025-10-20T21:58:19.300Z [ERROR] [api-gateway] request_id=abc123 user=user_1234 operation=processing duration=1222ms"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.Parse(line)
	}
}

func BenchmarkParseBatch(b *testing.B) {
	parser := NewLogParser()
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "2025-10-20T21:58:19.300Z [ERROR] [api-gateway] request_id=abc123 user=user_1234 operation=processing duration=1222ms"
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.ParseBatch(lines)
	}
}
