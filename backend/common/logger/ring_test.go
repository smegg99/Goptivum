// common/logger/ring_test.go

package logger_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/logger"
)

func line(ring *logger.Ring, level s99logger.Level, id string, attrs ...s99logger.Attr) {
	_ = ring.Write(context.Background(), s99logger.Record{
		Time: time.Now(), Level: level, MessageID: s99logger.MessageID(id), Message: id, Attrs: attrs,
	})
}

func ids(records []s99logger.Record) []string {
	out := []string{}
	for _, record := range records {
		out = append(out, string(record.MessageID))
	}
	return out
}

// The ring keeps the newest lines and hands them back newest first.
func TestTheRingKeepsTheNewestLines(t *testing.T) {
	ring := logger.NewRing(3)
	for i := 1; i <= 5; i++ {
		line(ring, s99logger.LevelInfo, fmt.Sprintf("line.%d", i))
	}
	if got := fmt.Sprint(ids(ring.Lines(s99logger.LevelDebug, 10))); got != "[line.5 line.4 line.3]" {
		t.Errorf("lines = %s, want the 3 newest, newest first", got)
	}
	if got := fmt.Sprint(ids(ring.Lines(s99logger.LevelDebug, 2))); got != "[line.5 line.4]" {
		t.Errorf("limited = %s", got)
	}
	if ring.Capacity() != 3 {
		t.Errorf("capacity = %d", ring.Capacity())
	}
}

// The level is a floor: warn lists warnings and errors.
func TestTheRingFiltersByTheLowestLevel(t *testing.T) {
	ring := logger.NewRing(10)
	line(ring, s99logger.LevelDebug, "debug")
	line(ring, s99logger.LevelInfo, "info")
	line(ring, s99logger.LevelWarn, "warn")
	line(ring, s99logger.LevelError, "error")
	if got := fmt.Sprint(ids(ring.Lines(s99logger.LevelWarn, 10))); got != "[error warn]" {
		t.Errorf("warn and up = %s", got)
	}
	if got := fmt.Sprint(ids(ring.Lines(s99logger.LevelError, 10))); got != "[error]" {
		t.Errorf("errors = %s", got)
	}
}

// A secret never reaches the ring: by the value's name, and a pairing link or
// a session token inside any value.
func TestTheRingRedactsSecrets(t *testing.T) {
	ring := logger.NewRing(10)
	attrs := []s99logger.Attr{
		s99logger.String("username", "goptivum-admin"),
		s99logger.String("password", "Tajne-Haslo-123"),
		s99logger.Err(errors.New(`bad link: parse "arrango://192.0.2.10:41826?token=abc123&pin=XYZ": invalid`)),
		s99logger.String("detail", "session ory_st_SyntheticToken1 ended"),
		s99logger.Int("minutes", 15),
		s99logger.Duration("took", 1500*time.Millisecond),
	}
	line(ring, s99logger.LevelWarn, "logs.bootstrapAdminCreated", attrs...)

	got := ring.Lines(s99logger.LevelDebug, 1)[0].Attrs
	values := map[string]any{}
	for _, attr := range got {
		values[attr.Key] = attr.Value
	}
	want := map[string]any{
		"username": "goptivum-admin",
		"password": "[redacted]",
		"error":    `bad link: parse "arrango://[redacted]": invalid`,
		"detail":   "session [redacted] ended",
		"minutes":  "15",
		"took":     "1.5s",
	}
	if fmt.Sprint(values) != fmt.Sprint(want) {
		t.Errorf("attrs = %v\nwant    %v", values, want)
	}
	if attrs[1].Value != "Tajne-Haslo-123" {
		t.Errorf("the caller's attrs were changed: %v", attrs[1])
	}
}

// Every logger the package builds writes to the one ring.
func TestTheServersLogReachesTheRing(t *testing.T) {
	if err := logger.Configure(config.Logging{Level: "INFO", NoColor: true}, "en"); err != nil {
		t.Fatalf("configure: %v", err)
	}
	logger.Log.Warn(s99logger.NewEvent(logger.EventObjectSweepFailed, s99logger.String("error", "synthetic")))
	lines := logger.Recent().Lines(s99logger.LevelWarn, 1)
	if len(lines) != 1 || lines[0].MessageID != logger.EventObjectSweepFailed {
		t.Fatalf("recent = %+v", lines)
	}
	if lines[0].Message == string(logger.EventObjectSweepFailed) {
		t.Errorf("message = %q, want the sentence", lines[0].Message)
	}
}
