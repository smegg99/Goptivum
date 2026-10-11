// api/v1/security.go

package v1

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/lockout"
	"github.com/smegg99/goptivum/backend/identity"
)

// Security serves how signing in is protected: the lockout the /kratos proxy
// enforces and the password rules Kratos enforces. Every route is admin-only.
type Security struct {
	changes  sync.Mutex
	settings *config.SettingsStore
	guard    *lockout.Guard
	// kratosConfig is Kratos's own configuration file; empty means the
	// password rules are not known.
	kratosConfig string
	// trail records lockout changes; nil records nothing.
	trail *audit.Log
}

// NewSecurity wires the Security routes. The guard must be the one the proxy
// asks, so a change applies at the next sign-in.
func NewSecurity(settings *config.SettingsStore, guard *lockout.Guard, kratosConfig string) *Security {
	return &Security{settings: settings, guard: guard, kratosConfig: kratosConfig}
}

// WithAudit records lockout changes in trail.
func (s *Security) WithAudit(trail *audit.Log) *Security {
	s.trail = trail
	return s
}

// LimitsOf is the stored lockout as the guard counts it.
func LimitsOf(stored config.Lockout) lockout.Limits {
	return lockout.Limits{
		AccountFailures: stored.AccountFailures,
		AddressFailures: stored.AddressFailures,
		Block:           time.Duration(stored.BlockMinutes) * time.Minute,
	}
}

// serves is nil-receiver safe: a Handlers built without this half answers 404.
func (s *Security) serves() bool { return s != nil && s.settings != nil && s.guard != nil }

// GetLockout is the lockout in force.
func (s *Security) GetLockout(_ context.Context, _ gen.GetLockoutRequestObject) (gen.GetLockoutResponseObject, error) {
	if !s.serves() {
		return gen.GetLockout404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	return gen.GetLockout200JSONResponse(toLockout(s.settings.Current().Lockout)), nil
}

// UpdateLockout changes the lockout, stores it and applies it at once.
func (s *Security) UpdateLockout(ctx context.Context, request gen.UpdateLockoutRequestObject) (gen.UpdateLockoutResponseObject, error) {
	if !s.serves() {
		return gen.UpdateLockout404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	body := request.Body
	if body == nil || (body.AccountFailures == nil && body.AddressFailures == nil && body.BlockMinutes == nil) {
		return gen.UpdateLockout422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "empty"))}, nil
	}
	s.changes.Lock()
	defer s.changes.Unlock()
	before := s.settings.Current().Lockout
	merged := before
	if body.AccountFailures != nil {
		merged.AccountFailures = *body.AccountFailures
	}
	if body.AddressFailures != nil {
		merged.AddressFailures = *body.AddressFailures
	}
	if body.BlockMinutes != nil {
		merged.BlockMinutes = *body.BlockMinutes
	}
	if field := LimitsOf(merged).Invalid(); field != "" {
		return gen.UpdateLockout422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(field, "invalid"))}, nil
	}

	saved, err := s.settings.SetLockout(merged)
	if err != nil {
		return nil, err
	}
	s.guard.SetLimits(LimitsOf(saved.Lockout))
	by, _ := callerID(ctx)
	logger.Log.Info(s99logger.NewEvent(logger.EventLockoutChanged,
		s99logger.Int("account_failures", saved.Lockout.AccountFailures),
		s99logger.Int("address_failures", saved.Lockout.AddressFailures),
		s99logger.Int("block_minutes", saved.Lockout.BlockMinutes),
		s99logger.String("by", by)))
	changed := map[string]string{}
	for _, limit := range []struct {
		name          string
		before, after int
	}{
		{"account_failures", before.AccountFailures, saved.Lockout.AccountFailures},
		{"address_failures", before.AddressFailures, saved.Lockout.AddressFailures},
		{"block_minutes", before.BlockMinutes, saved.Lockout.BlockMinutes},
	} {
		if limit.before != limit.after {
			changed[limit.name] = strconv.Itoa(limit.after)
		}
	}
	if len(changed) > 0 {
		audited(ctx, s.trail, audit.SettingChanged, audit.Target{Kind: audit.Setting, ID: "lockout"}, changed)
	}
	return gen.UpdateLockout200JSONResponse(toLockout(saved.Lockout)), nil
}

// GetPasswordRules reads Kratos's password rules from its configuration file.
func (s *Security) GetPasswordRules(_ context.Context, _ gen.GetPasswordRulesRequestObject) (gen.GetPasswordRulesResponseObject, error) {
	if !s.serves() {
		return gen.GetPasswordRules404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if s.kratosConfig == "" {
		return gen.GetPasswordRules200JSONResponse{Known: false}, nil
	}
	rules, err := identity.ReadPasswordRules(s.kratosConfig)
	if err != nil {
		logger.Log.Warn(s99logger.NewEvent(logger.EventPasswordRulesUnreadable,
			s99logger.String("path", s.kratosConfig), s99logger.String("error", err.Error())))
		return gen.GetPasswordRules200JSONResponse{Known: false}, nil
	}
	return gen.GetPasswordRules200JSONResponse{
		Known:                true,
		MinLength:            &rules.MinLength,
		IdentifierSimilarity: &rules.IdentifierSimilarity,
		BreachCheck:          &rules.BreachCheck,
		MaxBreaches:          &rules.MaxBreaches,
	}, nil
}

// toLockout is the stored lockout on the wire. Every limit is read on each
// sign-in, so a change applies now.
func toLockout(stored config.Lockout) gen.Lockout {
	return gen.Lockout{
		AccountFailures: stored.AccountFailures,
		AddressFailures: stored.AddressFailures,
		BlockMinutes:    stored.BlockMinutes,
		Applies:         gen.Now,
	}
}
