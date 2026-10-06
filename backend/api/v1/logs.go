// api/v1/logs.go

package v1

import (
	"context"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/logger"
)

// defaultLogLines is how many lines a listing returns when it names no limit.
const defaultLogLines = 200

// Logs serves the server's recent log lines. Its route is admin-only.
type Logs struct {
	ring *logger.Ring
	// translator words a line in the language asked for; nil keeps each
	// line in the language it was logged in.
	translator s99logger.Translator
}

// NewLogs wires the logs route over the ring the logger writes to.
func NewLogs(ring *logger.Ring, translator s99logger.Translator) *Logs {
	return &Logs{ring: ring, translator: translator}
}

// ListLogs is the newest lines at or above a level, newest first.
func (l *Logs) ListLogs(ctx context.Context, request gen.ListLogsRequestObject) (gen.ListLogsResponseObject, error) {
	if l == nil || l.ring == nil {
		return gen.ListLogs404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	refuse := func(field string) gen.ListLogsResponseObject {
		return gen.ListLogs422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(field, "invalid"))}
	}
	lowest := s99logger.LevelDebug
	if request.Params.Level != nil {
		level, ok := logLevels[*request.Params.Level]
		if !ok {
			return refuse("level"), nil
		}
		lowest = level
	}
	lang := ""
	if request.Params.Lang != nil {
		switch *request.Params.Lang {
		case gen.En, gen.Pl:
			lang = string(*request.Params.Lang)
		default:
			return refuse("lang"), nil
		}
	}
	limit := defaultLogLines
	if request.Params.Limit != nil && *request.Params.Limit > 0 {
		limit = *request.Params.Limit
	}
	limit = min(limit, l.ring.Capacity())

	lines := l.ring.Lines(lowest, limit)
	items := make([]gen.LogLine, 0, len(lines))
	for _, line := range lines {
		items = append(items, l.toLogLine(ctx, line, lang))
	}
	return gen.ListLogs200JSONResponse{Items: items, Capacity: l.ring.Capacity()}, nil
}

// logLevels are the contract's levels as the logger's.
var logLevels = map[gen.LogLevel]s99logger.Level{
	gen.LogLevelDebug: s99logger.LevelDebug,
	gen.LogLevelInfo:  s99logger.LevelInfo,
	gen.LogLevelWarn:  s99logger.LevelWarn,
	gen.LogLevelError: s99logger.LevelError,
}

// toLogLine is one held line on the wire, worded in lang when one is asked
// for and it has a sentence there. Its values are already redacted text.
func (l *Logs) toLogLine(ctx context.Context, line s99logger.Record, lang string) gen.LogLine {
	message := line.Message
	if lang != "" && l.translator != nil {
		if worded, err := l.translator.Translate(ctx, lang, s99logger.NewEvent(line.MessageID, line.Attrs...)); err == nil {
			message = worded
		}
	}
	out := gen.LogLine{
		Time: line.Time.UTC(), Level: gen.LogLevel(line.Level.String()),
		Event: string(line.MessageID), Message: message,
	}
	if len(line.Attrs) > 0 {
		fields := make(map[string]string, len(line.Attrs))
		for _, attr := range line.Attrs {
			if text, ok := attr.Value.(string); ok {
				fields[attr.Key] = text
			}
		}
		out.Fields = &fields
	}
	return out
}
