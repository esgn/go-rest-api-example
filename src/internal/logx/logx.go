// Package logx provides lightweight leveled logging on top of the standard log package.
package logx

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

// Level represents a log severity threshold.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var globalLevel int32 = int32(LevelInfo)

// SetLevel sets the global minimum log level.
func SetLevel(level Level) {
	atomic.StoreInt32(&globalLevel, int32(level))
}

// CurrentLevel returns the global minimum log level.
func CurrentLevel() Level {
	return Level(atomic.LoadInt32(&globalLevel))
}

// SetLevelFromString parses and sets the global level from string value.
// Supported values: debug, info, warn, error (case-insensitive).
func SetLevelFromString(raw string) error {
	level, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	SetLevel(level)
	return nil
}

// ParseLevel parses a level string.
func ParseLevel(raw string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return LevelDebug, nil
	case "", "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("invalid log level: %q", raw)
	}
}

// Debugf logs at debug level.
func Debugf(format string, args ...any) {
	logf(LevelDebug, format, args...)
}

// Infof logs at info level.
func Infof(format string, args ...any) {
	logf(LevelInfo, format, args...)
}

// Warnf logs at warn level.
func Warnf(format string, args ...any) {
	logf(LevelWarn, format, args...)
}

// Errorf logs at error level.
func Errorf(format string, args ...any) {
	logf(LevelError, format, args...)
}

func logf(level Level, format string, args ...any) {
	if level < CurrentLevel() {
		return
	}
	prefix := fmt.Sprintf("level=%s ", level.String())
	log.Printf(prefix+format, args...)
}

// String returns a stable uppercase label.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}
