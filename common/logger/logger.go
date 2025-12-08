// logger/logger.go
package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/viper"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var (
	currentLevel   = INFO
	logDir         = ""
	enableLogFiles = false
	noColor        = false
	logPrefix      = ""

	debugColor = color.New(color.FgCyan)
	infoColor  = color.New(color.FgGreen)
	warnColor  = color.New(color.FgYellow)
	errorColor = color.New(color.FgRed)
)

type Config struct {
	Dir         string
	EnableFiles bool
	NoColor     bool
	Prefix      string
	Level       string // optional: "DEBUG", "INFO", "WARN", "ERROR"
}

func Configure(cfg Config) {
	if cfg.Dir != "" {
		logDir = cfg.Dir
	}
	enableLogFiles = cfg.EnableFiles
	noColor = cfg.NoColor
	color.NoColor = noColor
	logPrefix = cfg.Prefix

	if cfg.Level != "" {
		if lvl, ok := parseLevel(cfg.Level); ok {
			currentLevel = lvl
		}
	}

	if enableLogFiles && logDir != "" {
		if err := os.MkdirAll(logDir, 0750); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		}
	}
}
func Initialize() {
	logDir = viper.GetString("LOGS_DIR")
	enableLogFiles = viper.GetBool("ENABLE_LOG_FILES")
	noColor = viper.GetBool("NO_COLOR")
	color.NoColor = noColor
	logPrefix = viper.GetString("LOG_PREFIX")

	if enableLogFiles && logDir != "" {
		if err := os.MkdirAll(logDir, 0750); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		}
	}
}

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "debug"
	case INFO:
		return "info"
	case WARN:
		return "warn"
	case ERROR:
		return "error"
	default:
		return "unknown"
	}
}

func SetLevel(level Level) {
	currentLevel = level
}

func SetLevelFromString(level string) {
	if lvl, ok := parseLevel(level); ok {
		currentLevel = lvl
	}
}

func parseLevel(s string) (Level, bool) {
	switch s {
	case "DEBUG", "debug":
		return DEBUG, true
	case "INFO", "info":
		return INFO, true
	case "WARN", "warn", "warning":
		return WARN, true
	case "ERROR", "error":
		return ERROR, true
	default:
		return INFO, false
	}
}

func SetLogDir(dir string) error {
	if dir != "" {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}
	logDir = dir
	return nil
}

func log(level Level, message string) {
	if level < currentLevel {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")

	prefix := ""
	if logPrefix != "" {
		prefix = fmt.Sprintf("[%s] ", logPrefix)
	}

	levelStr := level.String()

	var colorFunc *color.Color
	switch level {
	case DEBUG:
		colorFunc = debugColor
	case INFO:
		colorFunc = infoColor
	case WARN:
		colorFunc = warnColor
	case ERROR:
		colorFunc = errorColor
	}

	consoleMessage := fmt.Sprintf("%s %s[%s] %s", timestamp, prefix, levelStr, message)
	if _, err := colorFunc.Println(consoleMessage); err != nil {
		fmt.Println(consoleMessage)
	}

	if enableLogFiles && logDir != "" {
		fileMessage := consoleMessage
		writeToFile(fileMessage)
	}
}

func writeToFile(message string) {
	today := time.Now().Format("2006-01-02")
	filename := filepath.Join(logDir, fmt.Sprintf("%s.log", today))

	cleanLogDir := filepath.Clean(logDir)
	cleanFilename := filepath.Clean(filename)

	rel, err := filepath.Rel(cleanLogDir, cleanFilename)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) > 0 && rel[0] == '.' && rel[1] == '.' {
		fmt.Fprintf(os.Stderr, "invalid log file path: %s\n", filename)
		return
	}

	file, err := os.OpenFile(cleanFilename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close log file: %v\n", closeErr)
		}
	}()

	if _, err := io.WriteString(file, message+"\n"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write to log file: %v\n", err)
	}
}

func Debug(message string) {
	log(DEBUG, message)
}

func Debugf(format string, args ...interface{}) {
	Debug(fmt.Sprintf(format, args...))
}

func Info(message string) {
	log(INFO, message)
}

func Infof(format string, args ...interface{}) {
	Info(fmt.Sprintf(format, args...))
}

func Warn(message string) {
	log(WARN, message)
}

func Warnf(format string, args ...interface{}) {
	Warn(fmt.Sprintf(format, args...))
}

func Error(message string) {
	log(ERROR, message)
}

func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

func ErrorWithFields(message string, fields map[string]interface{}) {
	fieldsStr := ""
	for k, v := range fields {
		fieldsStr += fmt.Sprintf(" %s=%v", k, v)
	}
	Error(message + fieldsStr)
}

func Fatal(message string) {
	Error(message)
	os.Exit(1)
}

func Fatalf(format string, args ...interface{}) {
	Fatal(fmt.Sprintf(format, args...))
}

func LogRequest(method, path, clientIP string, latency time.Duration, statusCode int) {
	message := fmt.Sprintf("request: %s %s, ip: %s, status: %d, latency: %v",
		method, path, clientIP, statusCode, latency)

	switch {
	case statusCode >= 500:
		log(ERROR, message)
	case statusCode >= 400:
		log(WARN, message)
	default:
		log(INFO, message)
	}
}
