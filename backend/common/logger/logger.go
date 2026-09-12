// common/logger/logger.go

// Package logger owns the server's logger and its event catalog.
package logger

import (
	"os"
	"strings"

	"github.com/smegg99/s99logger"
	logi18n "github.com/smegg99/s99logger/i18n"
	"github.com/smegg99/s99logger/rotation"

	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/messages"
)

const serviceName = "goptivum-server"

// Log is what every package logs through. It starts as a console logger so a
// failure before the configuration is read still reaches the terminal.
var (
	Log      = console(false, s99logger.LevelInfo)
	fileSink *rotation.Sink
)

// console writes coloured, human-readable lines to stderr. Colour is on when
// stderr is a terminal and off when it is a pipe or a service manager's
// journal.
func console(noColor bool, level s99logger.Level) *s99logger.Logger {
	sink := s99logger.NewConsoleSink(os.Stderr)
	if noColor {
		sink.WithColor(false)
	}
	return s99logger.New(sink, s99logger.Options{
		Service: serviceName, MinLevel: level,
		Language: messages.DefaultLanguage, Translator: Translator(),
	})
}

// Configure rebuilds the logger from the configuration, adding a rotating file
// sink when the school wants one on disk. The language is the application's,
// because the log and the command line are one program's voice.
func Configure(settings config.Logging, language string) error {
	consoleSink := s99logger.NewConsoleSink(os.Stderr)
	if settings.NoColor {
		consoleSink.WithColor(false)
	}
	sinks := []s99logger.Sink{consoleSink}

	var nextFile *rotation.Sink
	if settings.EnableFiles {
		var err error
		nextFile, err = rotation.New(rotation.Options{
			Directory: settings.Dir, Filename: settings.LogName,
			MaxSizeMB: int(settings.MaxSizeMB), MaxBackups: int(settings.MaxBackups),
			MaxAgeDays: int(settings.MaxAgeDays), Compression: settings.Compression,
			LocalTime: settings.LocalTime,
		})
		if err != nil {
			return err
		}
		sinks = append(sinks, nextFile)
	}

	Log = s99logger.New(s99logger.MultiSink(sinks...), s99logger.Options{
		Service:    serviceName,
		MinLevel:   level(settings.Level, settings.Verbose),
		Language:   language,
		Translator: Translator(),
	})

	previous := fileSink
	fileSink = nextFile
	if previous != nil {
		return previous.Close()
	}
	return nil
}

// Close flushes the file sink. The console needs nothing.
func Close() error {
	sink := fileSink
	fileSink = nil
	if sink != nil {
		return sink.Close()
	}
	return nil
}

// Translator resolves an event id to the sentence one language writes for it.
// A missing catalog leaves the id, which is visibly wrong rather than blank.
func Translator() s99logger.Translator {
	translator, err := logi18n.New(logi18n.Options{
		FS: messages.Files, Files: messages.Names, DefaultLanguage: messages.DefaultLanguage,
	})
	if err != nil {
		return nil
	}
	return translator
}

func level(value string, verbose bool) s99logger.Level {
	if verbose {
		return s99logger.LevelDebug
	}
	switch strings.ToUpper(value) {
	case "DEBUG":
		return s99logger.LevelDebug
	case "WARN":
		return s99logger.LevelWarn
	case "ERROR":
		return s99logger.LevelError
	default:
		return s99logger.LevelInfo
	}
}
