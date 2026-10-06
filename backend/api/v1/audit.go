// api/v1/audit.go

package v1

import (
	"context"
	"strconv"
	"strings"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/audit"
)

// AuditTrail serves the audit trail. Its route is admin-only.
type AuditTrail struct {
	log *audit.Log
}

// NewAuditTrail wires the trail's route. A nil log answers 404.
func NewAuditTrail(log *audit.Log) *AuditTrail {
	return &AuditTrail{log: log}
}

// ListAudit pages the trail, newest first, narrowed to one kind when asked.
func (t *AuditTrail) ListAudit(ctx context.Context, request gen.ListAuditRequestObject) (gen.ListAuditResponseObject, error) {
	if t == nil || t.log == nil {
		return gen.ListAudit404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	var kind audit.Kind
	if request.Params.Kind != nil {
		kind = audit.Kind(*request.Params.Kind)
		if !knownKind(kind) {
			return gen.ListAudit422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("kind", "invalid"))}, nil
		}
	}
	page, limit := pageOf(request.Params.Page, request.Params.Limit)
	entries, total, err := t.log.Page(ctx, kind, page, limit)
	if err != nil {
		return gen.ListAudit503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	items := make([]gen.AuditEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, toAuditEntry(entry))
	}
	return gen.ListAudit200JSONResponse{Items: items, Page: page, Limit: limit, Total: total}, nil
}

func knownKind(kind audit.Kind) bool {
	for _, known := range audit.Kinds {
		if kind == known {
			return true
		}
	}
	return false
}

func toAuditEntry(entry audit.Entry) gen.AuditEntry {
	out := gen.AuditEntry{
		Id:      strconv.FormatInt(entry.ID, 10),
		At:      entry.At,
		Actor:   gen.AuditActor{Id: entry.Actor.ID},
		Action:  gen.AuditAction(entry.Action),
		Target:  gen.AuditTarget{Kind: gen.AuditKind(entry.Target.Kind), Id: entry.Target.ID},
		Details: entry.Details,
	}
	if entry.Actor.Name != "" {
		out.Actor.Name = pointerTo(entry.Actor.Name)
	}
	if entry.Target.Name != "" {
		out.Target.Name = pointerTo(entry.Target.Name)
	}
	return out
}

// actorOf is the signed-in account as the trail records it: its name, or its
// username when it has none.
func actorOf(ctx context.Context) audit.Actor {
	identity, ok := kratosauth.FromContext(ctx)
	if !ok {
		return audit.Actor{}
	}
	return audit.Actor{ID: identity.ID, Name: displayName(identity)}
}

// displayName is an account's name trait, or its username when it has none.
func displayName(identity *kratosauth.Identity) string {
	var name string
	if err := identity.Trait("name", &name); err != nil || strings.TrimSpace(name) == "" {
		_ = identity.Trait("username", &name)
	}
	return strings.TrimSpace(name)
}

// audited writes one entry for the signed-in account. The change it records
// has already happened, so a failed write is logged and changes no answer. It
// outlives the request: a client that leaves at once still leaves a trail.
func audited(ctx context.Context, trail *audit.Log, action audit.Action, target audit.Target, details map[string]string) {
	if trail == nil {
		return
	}
	if err := trail.Record(context.WithoutCancel(ctx), actorOf(ctx), action, target, details); err != nil {
		logger.Log.Error(s99logger.NewEvent(logger.EventAuditFailed,
			s99logger.String("action", string(action)), s99logger.Err(err)))
	}
}
