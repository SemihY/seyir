package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"seyir/internal/logger"
	"time"
)

// BatchConfigFile represents the structure of the batch configuration file
type BatchConfigFile struct {
	UltraLight struct {
		Enabled               bool `json:"enabled"`
		BufferSize            int  `json:"buffer_size"`
		ExportIntervalSeconds int  `json:"export_interval_seconds"`
		MaxMemoryMB           int  `json:"max_memory_mb"`
		UseUltraFastMode      bool `json:"use_ultra_fast_mode"`
	} `json:"ultra_light"`
	
	Compaction struct {
		Enabled               bool  `json:"enabled"`
		IntervalHours         int   `json:"interval_hours"`
		MinFilesForCompaction int   `json:"min_files_for_compaction"`
		MaxCompactedSizeMB    int64 `json:"max_compacted_size_mb"`
	} `json:"compaction"`
	
	Retention struct {
		Enabled        bool    `json:"enabled"`
		RetentionDays  int     `json:"retention_days"`
		CleanupHours   int     `json:"cleanup_hours"`
		KeepMinFiles   int     `json:"keep_min_files"`
		MaxTotalSizeGB float64 `json:"max_total_size_gb"`
	} `json:"retention"`
	
	Server struct {
		Port       string `json:"port"`
		EnableCORS bool   `json:"enable_cors"`
	} `json:"server"`
}

// DefaultConfigFile returns a default configuration
func DefaultConfigFile() *BatchConfigFile {
	return &BatchConfigFile{
		UltraLight: struct {
			Enabled               bool `json:"enabled"`
			BufferSize            int  `json:"buffer_size"`
			ExportIntervalSeconds int  `json:"export_interval_seconds"`
			MaxMemoryMB           int  `json:"max_memory_mb"`
			UseUltraFastMode      bool `json:"use_ultra_fast_mode"`
		}{
			Enabled:               true,
			BufferSize:            10000,
			ExportIntervalSeconds: 30,
			MaxMemoryMB:           50,
			UseUltraFastMode:      false,
		},
		Compaction: struct {
			Enabled               bool  `json:"enabled"`
			IntervalHours         int   `json:"interval_hours"`
			MinFilesForCompaction int   `json:"min_files_for_compaction"`
			MaxCompactedSizeMB    int64 `json:"max_compacted_size_mb"`
		}{
			Enabled:               true,
			IntervalHours:         4,
			MinFilesForCompaction: 10,
			MaxCompactedSizeMB:    50,
		},
		Retention: struct {
			Enabled        bool    `json:"enabled"`
			RetentionDays  int     `json:"retention_days"`
			CleanupHours   int     `json:"cleanup_hours"`
			KeepMinFiles   int     `json:"keep_min_files"`
			MaxTotalSizeGB float64 `json:"max_total_size_gb"`
		}{
			Enabled:        true,
			RetentionDays:  30,
			CleanupHours:   1,
			KeepMinFiles:   100,
			MaxTotalSizeGB: 10.0,
		},
		Server: struct {
			Port       string `json:"port"`
			EnableCORS bool   `json:"enable_cors"`
		}{
			Port:       "5555",
			EnableCORS: true,
		},
	}
}

// ToRetentionConfig converts the config file to a RetentionConfig
func (cf *BatchConfigFile) ToRetentionConfig() *RetentionConfig {
	return &RetentionConfig{
		Enabled:         cf.Retention.Enabled,
		RetentionDays:   cf.Retention.RetentionDays,
		CleanupInterval: time.Duration(cf.Retention.CleanupHours) * time.Hour,
		KeepMinFiles:    cf.Retention.KeepMinFiles,
		MaxTotalSizeGB:  cf.Retention.MaxTotalSizeGB,
	}
}

// LoadConfigFromFile loads batch configuration from a JSON file
func LoadConfigFromFile(configPath string) (*BatchConfigFile, error) {
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// File doesn't exist. Prepare default config.
		defaultConfig := DefaultConfigFile()

		// If process is started with piped stdin (pipe mode), avoid creating files
		if stat, sErr := os.Stdin.Stat(); sErr == nil {
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				// Running in pipe mode - return defaults without persisting to disk
				return defaultConfig, nil
			}
		}

		// Otherwise, attempt to create default config file on disk
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
	// Validate UltraLight settings (relaxed validation)
	if config.UltraLight.BufferSize < 10 {
		return fmt.Errorf("ultra_light.buffer_size must be at least 10")
	}
	
	if config.UltraLight.ExportIntervalSeconds < 1 {
		return fmt.Errorf("ultra_light.export_interval_seconds must be at least 1")
	}
	
	if config.UltraLight.MaxMemoryMB < 1 {
		return fmt.Errorf("ultra_light.max_memory_mb must be at least 1")
	}
	
	// Validate Compaction settings
	if config.Compaction.Enabled {
		if config.Compaction.IntervalHours < 1 {
			return fmt.Errorf("compaction.interval_hours must be at least 1")
		}
		
		if config.Compaction.MinFilesForCompaction < 2 {
			return fmt.Errorf("compaction.min_files_for_compaction must be at least 2")
		}
	}
	
	// Validate Retention settings
	if config.Retention.Enabled {
		if config.Retention.RetentionDays < 1 {
			return fmt.Errorf("retention.retention_days must be at least 1")
		}
		
		if config.Retention.CleanupHours < 1 {
			return fmt.Errorf("retention.cleanup_hours must be at least 1")
		}
		
		if config.Retention.KeepMinFiles < 1 {
			return fmt.Errorf("retention.keep_min_files must be at least 1")
		}
	}
	
	return nil
}

// InitializeBatchConfigFromFile initializes batch configuration from a file
func InitializeBatchConfigFromFile(configPath string) error {
	config, err := LoadConfigFromFile(configPath)
	if err != nil {
		return err
	}
	
	// Apply retention configuration
	retentionConfig := config.ToRetentionConfig()
	retentionManager := GetRetentionManager()
	retentionManager.UpdateConfig(retentionConfig)
	
	logger.Info("Configuration loaded from %s", configPath)
	logger.Info("UltraLight: enabled=%t, buffer=%d, interval=%ds, memory=%dMB, ultra_fast=%t", 
		config.UltraLight.Enabled, config.UltraLight.BufferSize,
		config.UltraLight.ExportIntervalSeconds, config.UltraLight.MaxMemoryMB, config.UltraLight.UseUltraFastMode)
	logger.Info("Compaction: enabled=%t, interval=%dh, min_files=%d, max_size=%dMB",
		config.Compaction.Enabled, config.Compaction.IntervalHours, 
		config.Compaction.MinFilesForCompaction, config.Compaction.MaxCompactedSizeMB)
	logger.Info("Retention: enabled=%t, days=%d, cleanup_hours=%d, keep_min=%d",
		config.Retention.Enabled, config.Retention.RetentionDays, 
		config.Retention.CleanupHours, config.Retention.KeepMinFiles)
	
	return nil
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
	
	// Apply retention configuration
	retentionConfig := config.ToRetentionConfig()
	retentionManager := GetRetentionManager()
	retentionManager.UpdateConfig(retentionConfig)
	
	// If process is started with piped stdin (pipe mode), avoid creating files
	if stat, sErr := os.Stdin.Stat(); sErr == nil {
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Running in pipe mode - do not create config file on disk
			return nil
		}
	}

	// Try to create default config file
	if err := SaveConfigToFile(DefaultConfigPath, config); err == nil {
		fmt.Printf("[INFO] Created default configuration file at %s\n", DefaultConfigPath)
	}

	fmt.Printf("[INFO] Using default UltraLight configuration\n")
	return nil
}