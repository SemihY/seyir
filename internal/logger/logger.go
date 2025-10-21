package logger

import (
	"log"
	"os"
	"seyir/internal/config"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

type Logger struct {
	logger *log.Logger
}

var std *Logger

func init() {
	std = &Logger{
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

// shouldLog checks if the message should be logged based on config
func (l *Logger) shouldLog(level Level) bool {
	// Always log WARN and ERROR
	if level >= WARN {
		return true
	}
	
	// Only log INFO and DEBUG if debug is enabled
	if level == INFO || level == DEBUG {
		return config.IsDebugEnabled()
	}
	
	return true
}

func (l *Logger) Debug(format string, v ...interface{}) {
	if l.shouldLog(DEBUG) {
		l.logger.Printf("[DEBUG] "+format, v...)
	}
}

func (l *Logger) Info(format string, v ...interface{}) {
	if l.shouldLog(INFO) {
		l.logger.Printf("[INFO] "+format, v...)
	}
}

func (l *Logger) Warn(format string, v ...interface{}) {
	if l.shouldLog(WARN) {
		l.logger.Printf("[WARN] "+format, v...)
	}
}

func (l *Logger) Error(format string, v ...interface{}) {
	if l.shouldLog(ERROR) {
		l.logger.Printf("[ERROR] "+format, v...)
	}
}

// Package-level functions
func Debug(format string, v ...interface{}) {
	std.Debug(format, v...)
}

func Info(format string, v ...interface{}) {
	std.Info(format, v...)
}

func Warn(format string, v ...interface{}) {
	std.Warn(format, v...)
}

func Error(format string, v ...interface{}) {
	std.Error(format, v...)
}
