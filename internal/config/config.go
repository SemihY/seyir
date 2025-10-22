package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config holds all application configuration
type Config struct {
	// Log collection settings
	Buffer struct {
		Size                 int  `json:"size"`                    // Buffer size (default: 10000)
		FlushIntervalSeconds int  `json:"flush_interval_seconds"`  // Flush interval (default: 30)
		MaxMemoryMB          int  `json:"max_memory_mb"`           // Max memory usage (default: 100)
		WorkerCount          int  `json:"worker_count"`            // Worker threads (default: 3)
	} `json:"buffer"`

	// File management
	Files struct {
		MaxSizeMB   int    `json:"max_size_mb"`   // Max file size (default: 50)
		MaxFiles    int    `json:"max_files"`     // Max files per session (default: 100)
		Compression string `json:"compression"`   // Compression type (default: "zstd")
	} `json:"files"`

	// Data retention
	Retention struct {
		Enabled        bool    `json:"enabled"`         // Enable retention (default: true)
		Days           int     `json:"days"`            // Keep logs for X days (default: 30)
		CleanupHours   int     `json:"cleanup_hours"`   // Cleanup interval (default: 24)
		KeepMinFiles   int     `json:"keep_min_files"`  // Always keep minimum files (default: 10)
		MaxTotalSizeGB float64 `json:"max_total_size_gb"` // Max total size (default: 10.0)
	} `json:"retention"`

	// Server settings
	Server struct {
		Port       string `json:"port"`        // Server port (default: "5555")
		EnableCORS bool   `json:"enable_cors"` // Enable CORS (default: true)
	} `json:"server"`

	// Collector settings
	Collector struct {
		MaxLineSizeBytes int `json:"max_line_size_bytes"` // Max log line size (default: 1MB)
	} `json:"collector"`

	// Debug settings
	Debug struct {
		Enabled bool `json:"enabled"` // Enable debug logging (default: false)
	} `json:"debug"`
}

var (
	globalConfig *Config
	configMutex  sync.RWMutex
	configLoaded bool
)

// Default returns the default configuration
func Default() *Config {
	return &Config{
		Buffer: struct {
			Size                 int `json:"size"`
			FlushIntervalSeconds int `json:"flush_interval_seconds"`
			MaxMemoryMB          int `json:"max_memory_mb"`
			WorkerCount          int `json:"worker_count"`
		}{
			Size:                 10000,
			FlushIntervalSeconds: 30,
			MaxMemoryMB:          100,
			WorkerCount:          3,
		},
		Files: struct {
			MaxSizeMB   int    `json:"max_size_mb"`
			MaxFiles    int    `json:"max_files"`
			Compression string `json:"compression"`
		}{
			MaxSizeMB:   50,
			MaxFiles:    100,
			Compression: "zstd",
		},
		Retention: struct {
			Enabled        bool    `json:"enabled"`
			Days           int     `json:"days"`
			CleanupHours   int     `json:"cleanup_hours"`
			KeepMinFiles   int     `json:"keep_min_files"`
			MaxTotalSizeGB float64 `json:"max_total_size_gb"`
		}{
			Enabled:        true,
			Days:           30,
			CleanupHours:   24,
			KeepMinFiles:   10,
			MaxTotalSizeGB: 10.0,
		},
		Server: struct {
			Port       string `json:"port"`
			EnableCORS bool   `json:"enable_cors"`
		}{
			Port:       "5555",
			EnableCORS: true,
		},
		Collector: struct {
			MaxLineSizeBytes int `json:"max_line_size_bytes"`
		}{
			MaxLineSizeBytes: 1024 * 1024, // 1MB
		},
		Debug: struct {
			Enabled bool `json:"enabled"`
		}{
			Enabled: false,
		},
	}
}

// Load loads configuration from file or uses defaults
func Load(configPath string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	// Start with defaults
	globalConfig = Default()

	// If config file doesn't exist, create it with defaults
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Don't create file in pipe mode
		if isPipeMode() {
			configLoaded = true
			return nil
		}

		// Create default config file
		if err := save(configPath, globalConfig); err != nil {
			return fmt.Errorf("failed to create default config: %v", err)
		}
		configLoaded = true
		return nil
	}

	// Load existing config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	// Parse and merge with defaults
	if err := json.Unmarshal(data, globalConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	configLoaded = true
	return nil
}

// Save saves configuration to file
func Save(configPath string) error {
	configMutex.RLock()
	defer configMutex.RUnlock()

	if globalConfig == nil {
		return fmt.Errorf("no configuration loaded")
	}

	return save(configPath, globalConfig)
}

// save is the internal save function (not thread-safe)
func save(configPath string, config *Config) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// Get returns a copy of the current configuration
func Get() *Config {
	configMutex.RLock()
	defer configMutex.RUnlock()

	if !configLoaded || globalConfig == nil {
		return Default()
	}

	// Return a copy to prevent external modifications
	copy := *globalConfig
	return &copy
}

// Set updates a configuration value
func Set(key, value string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	if globalConfig == nil {
		globalConfig = Default()
	}

	switch key {
	case "buffer_size":
		var val int
		if _, err := fmt.Sscanf(value, "%d", &val); err != nil {
			return fmt.Errorf("invalid buffer_size value: %s", value)
		}
		if val < 100 {
			return fmt.Errorf("buffer_size must be at least 100")
		}
		globalConfig.Buffer.Size = val

	case "flush_interval":
		var val int
		if _, err := fmt.Sscanf(value, "%d", &val); err != nil {
			return fmt.Errorf("invalid flush_interval value: %s", value)
		}
		if val < 1 {
			return fmt.Errorf("flush_interval must be at least 1")
		}
		globalConfig.Buffer.FlushIntervalSeconds = val

	case "max_memory_mb":
		var val int
		if _, err := fmt.Sscanf(value, "%d", &val); err != nil {
			return fmt.Errorf("invalid max_memory_mb value: %s", value)
		}
		if val < 10 {
			return fmt.Errorf("max_memory_mb must be at least 10")
		}
		globalConfig.Buffer.MaxMemoryMB = val

	case "retention_days":
		var val int
		if _, err := fmt.Sscanf(value, "%d", &val); err != nil {
			return fmt.Errorf("invalid retention_days value: %s", value)
		}
		if val < 1 {
			return fmt.Errorf("retention_days must be at least 1")
		}
		globalConfig.Retention.Days = val

	case "debug":
		switch value {
		case "true", "1", "enabled":
			globalConfig.Debug.Enabled = true
		case "false", "0", "disabled":
			globalConfig.Debug.Enabled = false
		default:
			return fmt.Errorf("invalid debug value: %s (use true/false)", value)
		}

	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}

	return nil
}

// GetPath returns the default configuration file path
func GetPath(dataDir string) string {
	return filepath.Join(dataDir, "config.json")
}

// IsDebugEnabled returns true if debug logging is enabled
func IsDebugEnabled() bool {
	return Get().Debug.Enabled
}

// isPipeMode checks if running in pipe mode
func isPipeMode() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}