package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BatchConfigFile represents the structure of the batch configuration file
type BatchConfigFile struct {
	BatchBuffer struct {
		FlushIntervalSeconds int  `json:"flush_interval_seconds"`
		BatchSize           int  `json:"batch_size"`
		MaxMemoryMB         int  `json:"max_memory_mb"`
		EnableAsync         bool `json:"enable_async"`
		WorkerCount         int  `json:"worker_count"`
	} `json:"batch_buffer"`
	
	FileRotation struct {
		MaxFileSizeMB int    `json:"max_file_size_mb"`
		MaxFiles      int    `json:"max_files"`
		Compression   string `json:"compression"`
	} `json:"file_rotation"`
	
	Retention struct {
		Enabled         bool    `json:"enabled"`
		RetentionDays   int     `json:"retention_days"`
		CleanupHours    int     `json:"cleanup_hours"`
		MaxFilesPerScan int     `json:"max_files_per_scan"`
		DryRun          bool    `json:"dry_run"`
		KeepMinFiles    int     `json:"keep_min_files"`
		MaxTotalSizeGB  float64 `json:"max_total_size_gb"`
	} `json:"retention"`
	
	ProcessDirs struct {
		UseProcessName bool   `json:"use_process_name"`
		BasePath       string `json:"base_path"`
	} `json:"process_dirs"`
	
	Server struct {
		Port           string `json:"port"`
		EnableCORS     bool   `json:"enable_cors"`
		EnableDebug    bool   `json:"enable_debug"`
	} `json:"server"`
}

// DefaultConfigFile returns a default configuration
func DefaultConfigFile() *BatchConfigFile {
	return &BatchConfigFile{
		BatchBuffer: struct {
			FlushIntervalSeconds int  `json:"flush_interval_seconds"`
			BatchSize           int  `json:"batch_size"`
			MaxMemoryMB         int  `json:"max_memory_mb"`
			EnableAsync         bool `json:"enable_async"`
			WorkerCount         int  `json:"worker_count"`
		}{
			FlushIntervalSeconds: 5,
			BatchSize:           10000,
			MaxMemoryMB:         100,
			EnableAsync:         true,
			WorkerCount:         3,
		},
		FileRotation: struct {
			MaxFileSizeMB int    `json:"max_file_size_mb"`
			MaxFiles      int    `json:"max_files"`
			Compression   string `json:"compression"`
		}{
			MaxFileSizeMB: 50,
			MaxFiles:      10,
			Compression:   "zstd",
		},
		Retention: struct {
			Enabled         bool    `json:"enabled"`
			RetentionDays   int     `json:"retention_days"`
			CleanupHours    int     `json:"cleanup_hours"`
			MaxFilesPerScan int     `json:"max_files_per_scan"`
			DryRun          bool    `json:"dry_run"`
			KeepMinFiles    int     `json:"keep_min_files"`
			MaxTotalSizeGB  float64 `json:"max_total_size_gb"`
		}{
			Enabled:         false, // Disabled by default
			RetentionDays:   30,
			CleanupHours:    1, // Check every hour
			MaxFilesPerScan: 1000,
			DryRun:          false,
			KeepMinFiles:    5,
			MaxTotalSizeGB:  10.0,
		},
		ProcessDirs: struct {
			UseProcessName bool   `json:"use_process_name"`
			BasePath       string `json:"base_path"`
		}{
			UseProcessName: true,
			BasePath:       "logs",
		},
		Server: struct {
			Port           string `json:"port"`
			EnableCORS     bool   `json:"enable_cors"`
			EnableDebug    bool   `json:"enable_debug"`
		}{
			Port:           "5555",
			EnableCORS:     true,
			EnableDebug:    false,
		},
	}
}

// ToBatchConfig converts the config file to a BatchConfig
func (cf *BatchConfigFile) ToBatchConfig() *BatchConfig {
	return &BatchConfig{
		FlushInterval: time.Duration(cf.BatchBuffer.FlushIntervalSeconds) * time.Second,
		BatchSize:     cf.BatchBuffer.BatchSize,
		MaxMemory:     int64(cf.BatchBuffer.MaxMemoryMB) * 1024 * 1024,
		EnableAsync:   cf.BatchBuffer.EnableAsync,
	}
}

// ToRetentionConfig converts the config file to a RetentionConfig
func (cf *BatchConfigFile) ToRetentionConfig() *RetentionConfig {
	return &RetentionConfig{
		Enabled:          cf.Retention.Enabled,
		RetentionDays:    cf.Retention.RetentionDays,
		CleanupInterval:  time.Duration(cf.Retention.CleanupHours) * time.Hour,
		MaxFilesPerScan:  cf.Retention.MaxFilesPerScan,
		DryRun:           cf.Retention.DryRun,
		KeepMinFiles:     cf.Retention.KeepMinFiles,
		MaxTotalSizeGB:   cf.Retention.MaxTotalSizeGB,
	}
}

// LoadConfigFromFile loads batch configuration from a JSON file
func LoadConfigFromFile(configPath string) (*BatchConfigFile, error) {
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config file if it doesn't exist
		defaultConfig := DefaultConfigFile()
		if err := SaveConfigToFile(configPath, defaultConfig); err != nil {
			return nil, fmt.Errorf("failed to create default config file: %v", err)
		}
		return defaultConfig, nil
	}
	
	// Read existing config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}
	
	var config BatchConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	
	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}
	
	return &config, nil
}

// SaveConfigToFile saves the configuration to a JSON file
func SaveConfigToFile(configPath string, config *BatchConfigFile) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}
	
	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}
	
	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}
	
	return nil
}

// validateConfig validates the configuration values
func validateConfig(config *BatchConfigFile) error {
	if config.BatchBuffer.FlushIntervalSeconds < 1 {
		return fmt.Errorf("flush_interval_seconds must be at least 1")
	}
	
	if config.BatchBuffer.BatchSize < 1 {
		return fmt.Errorf("batch_size must be at least 1")
	}
	
	if config.BatchBuffer.MaxMemoryMB < 1 {
		return fmt.Errorf("max_memory_mb must be at least 1")
	}
	
	if config.BatchBuffer.WorkerCount < 1 {
		return fmt.Errorf("worker_count must be at least 1")
	}
	
	if config.FileRotation.MaxFileSizeMB < 1 {
		return fmt.Errorf("max_file_size_mb must be at least 1")
	}
	
	if config.FileRotation.MaxFiles < 1 {
		return fmt.Errorf("max_files must be at least 1")
	}
	
	if config.Retention.RetentionDays < 1 && config.Retention.Enabled {
		return fmt.Errorf("retention_days must be at least 1 when retention is enabled")
	}
	
	if config.Retention.CleanupHours < 1 && config.Retention.Enabled {
		return fmt.Errorf("cleanup_hours must be at least 1 when retention is enabled")
	}
	
	if config.Retention.KeepMinFiles < 1 {
		return fmt.Errorf("keep_min_files must be at least 1")
	}
	
	return nil
}

// InitializeBatchConfigFromFile initializes batch configuration from a file
func InitializeBatchConfigFromFile(configPath string) error {
	config, err := LoadConfigFromFile(configPath)
	if err != nil {
		return err
	}
	
	// Apply batch buffer configuration
	batchConfig := config.ToBatchConfig()
	globalBatchManager.SetDefaultConfig(batchConfig)
	
	// Apply retention configuration
	retentionConfig := config.ToRetentionConfig()
	retentionManager := GetRetentionManager()
	retentionManager.UpdateConfig(retentionConfig)
	
	fmt.Printf("[INFO] Configuration loaded from %s\n", configPath)
	fmt.Printf("[INFO] Batch: flush=%ds, size=%d, memory=%dMB, async=%t, workers=%d\n", 
		config.BatchBuffer.FlushIntervalSeconds, config.BatchBuffer.BatchSize, 
		config.BatchBuffer.MaxMemoryMB, config.BatchBuffer.EnableAsync, config.BatchBuffer.WorkerCount)
	fmt.Printf("[INFO] Rotation: max_size=%dMB, max_files=%d, compression=%s\n",
		config.FileRotation.MaxFileSizeMB, config.FileRotation.MaxFiles, config.FileRotation.Compression)
	fmt.Printf("[INFO] Retention: enabled=%t, days=%d, cleanup_hours=%d, dry_run=%t\n",
		config.Retention.Enabled, config.Retention.RetentionDays, 
		config.Retention.CleanupHours, config.Retention.DryRun)
	
	return nil
}

// getCurrentCompressionSetting returns the current compression setting from config
func getCurrentCompressionSetting() string {
	// Try to load current config
	config, err := LoadConfigFromFile(DefaultConfigPath)
	if err != nil {
		// Fallback to default if config loading fails
		return "zstd"
	}
	
	compression := config.FileRotation.Compression
	if compression == "" {
		return "zstd" // Default fallback
	}
	return compression
}

// GetConfigExample returns an example configuration as a string
func GetConfigExample() string {
	example := DefaultConfigFile()
	data, _ := json.MarshalIndent(example, "", "  ")
	return string(data)
}

// Configuration file paths
const (
	DefaultConfigPath     = "config/config.json"
	UserConfigPath        = "~/.seyir/config.json"
	SystemConfigPath      = "/etc/seyir/config.json"
)

// LoadDefaultConfig attempts to load configuration from standard locations
func LoadDefaultConfig() error {
	// Try loading from various locations in order of preference
	configPaths := []string{
		DefaultConfigPath,
		UserConfigPath,
		SystemConfigPath,
	}
	
	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			return InitializeBatchConfigFromFile(path)
		}
	}
	
	// If no config file found, use defaults and create one
	config := DefaultConfigFile()
	
	// Apply batch buffer configuration
	batchConfig := config.ToBatchConfig()
	globalBatchManager.SetDefaultConfig(batchConfig)
	
	// Apply retention configuration
	retentionConfig := config.ToRetentionConfig()
	retentionManager := GetRetentionManager()
	retentionManager.UpdateConfig(retentionConfig)
	
	// Try to create default config file
	if err := SaveConfigToFile(DefaultConfigPath, config); err == nil {
		fmt.Printf("[INFO] Created default configuration file at %s\n", DefaultConfigPath)
	}
	
	fmt.Printf("[INFO] Using default configuration\n")
	return nil
}