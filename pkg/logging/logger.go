package logging

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Level defines the severity of a log message.
type Level int

const (
	// DEBUG is the most verbose log level.
	DEBUG Level = iota
	// INFO is for general operational messages.
	INFO
	// WARN is for potentially harmful situations.
	WARN
	// ERROR is for error events.
	ERROR
)

// String returns the string representation of a log level.
func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Field is a structured log field as a key-value pair.
type Field struct {
	Key   string
	Value any
}

// String creates a string field.
func String(key, val string) Field {
	return Field{Key: key, Value: val}
}

// Int creates an integer field.
func Int(key string, val int) Field {
	return Field{Key: key, Value: val}
}

// Err creates an error field with the key "error".
func Err(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: "<nil>"}
	}
	return Field{Key: "error", Value: err.Error()}
}

// Duration creates a duration field.
func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Value: val.String()}
}

// Logger provides structured logging with levels.
type Logger struct {
	level  Level
	prefix string
	mu     sync.Mutex
}

// New creates a new Logger with the given prefix.
// Default level is INFO.
func New(prefix string) *Logger {
	return &Logger{
		level:  INFO,
		prefix: prefix,
	}
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Debug logs a message at DEBUG level.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.log(DEBUG, msg, fields)
}

// Info logs a message at INFO level.
func (l *Logger) Info(msg string, fields ...Field) {
	l.log(INFO, msg, fields)
}

// Warn logs a message at WARN level.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.log(WARN, msg, fields)
}

// Error logs a message at ERROR level.
func (l *Logger) Error(msg string, fields ...Field) {
	l.log(ERROR, msg, fields)
}

// log writes a structured log line to stderr if the level is at or above the
// configured minimum level.
func (l *Logger) log(level Level, msg string, fields []Field) {
	l.mu.Lock()
	currentLevel := l.level
	l.mu.Unlock()

	if level < currentLevel {
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	var sb strings.Builder
	sb.WriteString(ts)
	sb.WriteString(" ")
	sb.WriteString(level.String())
	sb.WriteString(" [")
	sb.WriteString(l.prefix)
	sb.WriteString("] ")
	sb.WriteString(msg)

	for _, f := range fields {
		sb.WriteString(" ")
		sb.WriteString(f.Key)
		sb.WriteString("=")
		sb.WriteString(fmt.Sprintf("%v", f.Value))
	}

	sb.WriteString("\n")
	fmt.Fprint(os.Stderr, sb.String())
}
