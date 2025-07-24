// Copyright (c) 2025 Dynatrace LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"log"
	"os"
	"strconv"
)

type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
)

// Centralized logging for the application
type Logger struct {
	level  LogLevel
	logger *log.Logger
}

var (
	appLogger *Logger
)

// Initializes the global logger based on environment variables
func init() {
	level := LogLevelInfo

	// Check DEBUG environment variable
	debugEnv := os.Getenv("DEBUG")
	if debugEnv != "" {
		if enabled, err := strconv.ParseBool(debugEnv); err == nil && enabled {
			level = LogLevelDebug
		} else if debugEnv != "false" {
			// If DEBUG is set to any non-boolean value, enable debug
			level = LogLevelDebug
		}
	}

	// Check LOG_LEVEL environment variable for more granular control
	if logLevelEnv := os.Getenv("LOG_LEVEL"); logLevelEnv != "" {
		switch logLevelEnv {
		case "DEBUG", "debug":
			level = LogLevelDebug
		case "INFO", "info":
			level = LogLevelInfo
		case "WARN", "warn", "WARNING", "warning":
			level = LogLevelWarn
		case "ERROR", "error":
			level = LogLevelError
		}
	}

	appLogger = &Logger{
		level:  level,
		logger: log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lshortfile),
	}

	if level == LogLevelDebug {
		appLogger.Info("Debug logging enabled")
	}
}

func GetLogger() *Logger {
	return appLogger
}

func (l *Logger) Debug(format string, args ...interface{}) {
	if l.level >= LogLevelDebug {
		l.logger.Printf("DEBUG: "+format, args...)
	}
}

func (l *Logger) Info(format string, args ...interface{}) {
	if l.level >= LogLevelInfo {
		l.logger.Printf("INFO: "+format, args...)
	}
}

func (l *Logger) Warn(format string, args ...interface{}) {
	if l.level >= LogLevelWarn {
		l.logger.Printf("WARN: "+format, args...)
	}
}

func (l *Logger) Error(format string, args ...interface{}) {
	if l.level >= LogLevelError {
		l.logger.Printf("ERROR: "+format, args...)
	}
}

// Functions for global logger access
func Debug(format string, args ...interface{}) {
	appLogger.Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	appLogger.Info(format, args...)
}

func Warn(format string, args ...interface{}) {
	appLogger.Warn(format, args...)
}

func Error(format string, args ...interface{}) {
	appLogger.Error(format, args...)
}
