// common/logger/ring.go

package logger

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/smegg99/s99logger"
)

// RecentLines is how many lines the server keeps in memory for Backend > Logs.
const RecentLines = 1000

// recent is the ring every logger this package builds writes to.
var recent = NewRing(RecentLines)

// Recent is the server's newest log lines.
func Recent() *Ring { return recent }

// Redacted replaces a value that could be a secret.
const Redacted = "[redacted]"

// secretNames are attribute names whose value is never kept.
var secretNames = map[string]bool{
	"password": true, "passphrase": true, "secret": true, "token": true,
	"dsn": true, "uri": true, "link": true, "cookie": true, "authorization": true,
}

// secretValues are secrets that can sit inside any value: a pairing link, which
// an error from parsing one quotes whole, and a Kratos session token.
var secretValues = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile(`arrango://[^\s"']+`), "arrango://" + Redacted},
	{regexp.MustCompile(`ory_st_[A-Za-z0-9_-]+`), Redacted},
}

// Ring keeps the newest records in memory, each value as text with secrets
// redacted. It is a sink, so it holds what the logger's level lets through.
type Ring struct {
	mu    sync.Mutex
	lines []s99logger.Record
	next  int
	full  bool
}

// NewRing holds up to capacity records.
func NewRing(capacity int) *Ring {
	return &Ring{lines: make([]s99logger.Record, capacity)}
}

// Capacity is how many records the ring holds.
func (r *Ring) Capacity() int { return len(r.lines) }

// Write keeps a redacted copy of rec, dropping the oldest when full.
func (r *Ring) Write(_ context.Context, rec s99logger.Record) error {
	if len(r.lines) == 0 {
		return nil
	}
	attrs := make([]s99logger.Attr, 0, len(rec.Attrs))
	for _, attr := range rec.Attrs {
		attrs = append(attrs, s99logger.Attr{Key: attr.Key, Value: redact(attr)})
	}
	rec.Attrs = attrs

	r.mu.Lock()
	r.lines[r.next] = rec
	r.next = (r.next + 1) % len(r.lines)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
	return nil
}

// Lines returns up to limit records at or above min, newest first.
func (r *Ring) Lines(min s99logger.Level, limit int) []s99logger.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	held := r.next
	if r.full {
		held = len(r.lines)
	}
	out := []s99logger.Record{}
	for i := 0; i < held && len(out) < limit; i++ {
		rec := r.lines[(r.next-1-i+len(r.lines))%len(r.lines)]
		if rec.Level >= min {
			out = append(out, rec)
		}
	}
	return out
}

// redact is an attribute's value as text, with any secret in it replaced.
func redact(attr s99logger.Attr) string {
	if secretNames[strings.ToLower(attr.Key)] {
		return Redacted
	}
	text := fmt.Sprint(attr.Value)
	for _, secret := range secretValues {
		text = secret.pattern.ReplaceAllString(text, secret.with)
	}
	return text
}
