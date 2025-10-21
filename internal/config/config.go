package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// GlobalConfig holds all application configuration
type GlobalConfig struct {
	BatchBuffer   BatchBufferConfig   `json:"batch_buffer"`
	FileRotation  FileRotationConfig  `json:"file_rotation"`
	Retention     RetentionConfig     `json:"retention"`
	ProcessDirs   ProcessDirsConfig   `json:"process_dirs"`
	Collector     CollectorConfig     `json:"collector"`
	Server        ServerConfig        `json:"server"`
	Debug         DebugConfig         `json:"debug"`
}

type BatchBufferConfig struct {
	FlushIntervalSeconds int  `json:"flush_interval_seconds"`
	BatchSize            int  `json:"batch_size"`
	MaxMemoryMB          int  `json:"max_memory_mb"`
	EnableAsync          bool `json:"enable_async"`
	WorkerCount          int  `json:"worker_count"`
}

type FileRotationConfig struct {
	MaxFileSizeMB int    `json:"max_file_size_mb"`
	MaxFiles      int    `json:"max_files"`
	Compression   string `json:"compression"`
}

type RetentionConfig struct {
	Enabled           bool    `json:"enabled"`
	RetentionDays     int     `json:"retention_days"`
	CleanupHours      int     `json:"cleanup_hours"`
	MaxFilesPerScan   int     `json:"max_files_per_scan"`
	DryRun            bool    `json:"dry_run"`
	KeepMinFiles      int     `json:"keep_min_files"`
	MaxTotalSizeGB    float64 `json:"max_total_size_gb"`
}

type ProcessDirsConfig struct {
	UseProcessName bool   `json:"use_process_name"`
	BasePath       string `json:"base_path"`
}

type CollectorConfig struct {
	MaxLineSizeBytes int `json:"max_line_size_bytes"` // Maximum log line size (default 1MB)
}

type ServerConfig struct {
	Port        string `json:"port"`
	EnableCORS  bool   `json:"enable_cors"`
	EnableDebug bool   `json:"enable_debug"`
}

type DebugConfig struct {
	EnableQueryDebug  bool `json:"enable_query_debug"`
	EnableBatchDebug  bool `json:"enable_batch_debug"`
	EnableServerDebug bool `json:"enable_server_debug"`
	EnableDBDebug     bool `json:"enable_db_debug"`
}

var (
	globalConfig *GlobalConfig
	configMutex  sync.RWMutex
	configLoaded bool
)

// DefaultConfig returns the default configuration
func DefaultConfig() *GlobalConfig {
	return &GlobalConfig{
		BatchBuffer: BatchBufferConfig{
			FlushIntervalSeconds: 5,
			BatchSize:            10000,
			MaxMemoryMB:          100,
			EnableAsync:          true,
			WorkerCount:          3,
		},
		FileRotation: FileRotationConfig{
			MaxFileSizeMB: 50,
			MaxFiles:      50,
			Compression:   "zstd",
		},
		Retention: RetentionConfig{
			Enabled:           true,
			RetentionDays:     30,
			CleanupHours:      1,
			MaxFilesPerScan:   1000,
			DryRun:            false,
			KeepMinFiles:      100,
			MaxTotalSizeGB:    10.0,
		},
		ProcessDirs: ProcessDirsConfig{
			UseProcessName: true,
			BasePath:       "logs",
		},
		Collector: CollectorConfig{
			MaxLineSizeBytes: 1024 * 1024, // 1MB default
		},
		Server: ServerConfig{
			Port:        "5555",
			EnableCORS:  true,
			EnableDebug: false,
		},
		Debug: DebugConfig{
			EnableQueryDebug:  false,
			EnableBatchDebug:  false,
			EnableServerDebug: false,
			EnableDBDebug:     false,
		},
	}
}

// LoadConfig loads configuration from file with fallback to defaults
func LoadConfig(configPath string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	// Start with defaults
	globalConfig = DefaultConfig()

	// Try to load from file
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Config file doesn't exist, use defaults
		configLoaded = true
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	// Merge with defaults (overwrites only specified fields)
	if err := json.Unmarshal(data, globalConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	configLoaded = true
	return nil
}

// SaveConfig saves the current configuration to file
func SaveConfig(configPath string) error {
	configMutex.RLock()
	defer configMutex.RUnlock()

	if globalConfig == nil {
		return fmt.Errorf("no configuration loaded")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	data, err := json.MarshalIndent(globalConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// Get returns a copy of the current configuration
func Get() *GlobalConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()

	if !configLoaded || globalConfig == nil {
		return DefaultConfig()
	}

	// Return a copy to prevent external modifications
	copy := *globalConfig
	return &copy
}

// GetBatchBuffer returns batch buffer configuration
func GetBatchBuffer() BatchBufferConfig {
	return Get().BatchBuffer
}

// GetFileRotation returns file rotation configuration
func GetFileRotation() FileRotationConfig {
	return Get().FileRotation
}

// GetRetention returns retention configuration  
func GetRetention() RetentionConfig {
	return Get().Retention
}

// GetProcessDirs returns process directories configuration
func GetProcessDirs() ProcessDirsConfig {
	return Get().ProcessDirs
}

// GetCollector returns collector configuration
func GetCollector() CollectorConfig {
	return Get().Collector
}

// GetServer returns server configuration
func GetServer() ServerConfig {
	return Get().Server
}

// GetDebug returns debug configuration
func GetDebug() DebugConfig {
	return Get().Debug
}

// IsDebugEnabled returns true if any debug mode is enabled
func IsDebugEnabled() bool {
	debug := GetDebug()
	return debug.EnableQueryDebug || debug.EnableBatchDebug || 
		   debug.EnableServerDebug || debug.EnableDBDebug
}

// IsQueryDebugEnabled returns true if query debug is enabled
func IsQueryDebugEnabled() bool {
	return GetDebug().EnableQueryDebug
}

// IsBatchDebugEnabled returns true if batch debug is enabled
func IsBatchDebugEnabled() bool {
	return GetDebug().EnableBatchDebug
}

// IsServerDebugEnabled returns true if server debug is enabled
func IsServerDebugEnabled() bool {
	return GetDebug().EnableServerDebug
}

// IsDBDebugEnabled returns true if DB debug is enabled
func IsDBDebugEnabled() bool {
	return GetDebug().EnableDBDebug
}

// UpdateBatchBuffer updates batch buffer configuration
func UpdateBatchBuffer(config BatchBufferConfig) {
	configMutex.Lock()
	defer configMutex.Unlock()
	
	if globalConfig != nil {
		globalConfig.BatchBuffer = config
	}
}

// UpdateDebug updates debug configuration
func UpdateDebug(config DebugConfig) {
	configMutex.Lock()
	defer configMutex.Unlock()
	
	if globalConfig != nil {
		globalConfig.Debug = config
	}
}

// IsLoaded returns true if configuration has been loaded
func IsLoaded() bool {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return configLoaded
}

// GetDefaultConfigPath returns the default configuration file path
func GetDefaultConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "config", "config.json")
}