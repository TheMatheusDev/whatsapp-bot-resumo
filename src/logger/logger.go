package logger

import (
	"fmt"
	"io"
	"log"
	"os"
)

// SimpleLogger implements types.Logger interface with file and console output
type SimpleLogger struct {
	logger  *log.Logger
	logFile *os.File
}

// New creates a new logger. When apiLogs is true, output is tee'd to
// bot_debug.log in addition to stdout. When false, only stdout is used.
func New(apiLogs bool) (*SimpleLogger, error) {
	var multiWriter io.Writer = os.Stdout
	var logFile *os.File

	if apiLogs {
		f, err := os.OpenFile("bot_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		logFile = f
		multiWriter = io.MultiWriter(os.Stdout, logFile)
	}

	l := log.New(multiWriter, "", log.LstdFlags)

	return &SimpleLogger{
		logger:  l,
		logFile: logFile,
	}, nil
}

func (l *SimpleLogger) Debug(msg string, fields ...interface{}) {
	l.logger.Printf("[DEBUG] %s %v", msg, fields)
}

func (l *SimpleLogger) Info(msg string, fields ...interface{}) {
	l.logger.Printf("[INFO] %s %v", msg, fields)
}

func (l *SimpleLogger) Warn(msg string, fields ...interface{}) {
	l.logger.Printf("[WARN] %s %v", msg, fields)
}

func (l *SimpleLogger) Error(msg string, fields ...interface{}) {
	l.logger.Printf("[ERROR] %s %v", msg, fields)
}

func (l *SimpleLogger) Fatal(msg string, fields ...interface{}) {
	l.logger.Fatalf("[FATAL] %s %v", msg, fields)
}

func (l *SimpleLogger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}
