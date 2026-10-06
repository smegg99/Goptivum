// api/v1/serversettings.go

package v1

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/plans"
)

// The ranges settings.cue states, checked here first so a refusal names its
// field; the schema checks them again before the file is written.
const (
	maxPublicAddresses = 16
	maxAddressLength   = 255
	minUploadBytes     = 1024
	maxUploadBytes     = 256 << 20
	maxRatePerMinute   = 100000
	maxTrashDays       = 3650
)

// publicAddress is settings.cue's #PublicAddress.
var publicAddress = regexp.MustCompile(`^https?://[^\s/?#@]+(/[^\s?#]*)?$`)

// Running is what the settings that apply at a restart were when this process
// started, which is what it serves with until the next one.
type Running struct {
	MaximumUploadBytes int64
	RateLimitPerMinute int
}

// ServerSettings serves Backend > Server: the settings an administrator
// changes while the server runs. Every route is admin-only.
type ServerSettings struct {
	store   *config.SettingsStore
	index   *plans.Repository
	running Running
	trail   *audit.Log
	// changing holds one change at a time from its read to its write, so two
	// administrators changing different settings both keep theirs.
	changing sync.Mutex
}

// NewServerSettings wires the settings routes. index takes the new caps at
// once; running is what this process started with.
func NewServerSettings(store *config.SettingsStore, index *plans.Repository, running Running,
	trail *audit.Log) *ServerSettings {
	return &ServerSettings{store: store, index: index, running: running, trail: trail}
}

func (s *ServerSettings) serves() bool { return s != nil && s.store != nil && s.index != nil }

// GetServerSettings is the stored settings and when each applies.
func (s *ServerSettings) GetServerSettings(_ context.Context, _ gen.GetServerSettingsRequestObject) (gen.GetServerSettingsResponseObject, error) {
	if !s.serves() {
		return gen.GetServerSettings404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	return gen.GetServerSettings200JSONResponse(s.body(s.store.Current())), nil
}

// UpdateServerSettings changes the settings sent, stores them and applies
// those that apply now.
func (s *ServerSettings) UpdateServerSettings(ctx context.Context, request gen.UpdateServerSettingsRequestObject) (gen.UpdateServerSettingsResponseObject, error) {
	if !s.serves() {
		return gen.UpdateServerSettings404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	refuse := func(field, code string) gen.UpdateServerSettingsResponseObject {
		return gen.UpdateServerSettings422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(field, code))}
	}
	body := request.Body
	if body == nil || (body.PublicAddresses == nil && body.MaximumUploadBytes == nil && body.RateLimitPerMinute == nil &&
		body.TrashDays == nil && body.BackupsKeep == nil && body.ResultsKeep == nil) {
		return refuse("body", "empty"), nil
	}

	s.changing.Lock()
	defer s.changing.Unlock()
	before := s.store.Current()
	server, projects := before.Server, before.Projects
	if body.PublicAddresses != nil {
		addresses, field, code := cleanAddresses(*body.PublicAddresses)
		if field != "" {
			return refuse(field, code), nil
		}
		server.PublicAddresses = addresses
	}
	for _, number := range []struct {
		field    string
		value    *int
		min, max int
		into     *int
	}{
		{"rate_limit_per_minute", body.RateLimitPerMinute, 0, maxRatePerMinute, &server.RateLimitPerMinute},
		{"trash_days", body.TrashDays, 1, maxTrashDays, &projects.TrashDays},
		{"backups_keep", body.BackupsKeep, 1, plans.MaxBackupsKeep, &projects.BackupsKeep},
		{"results_keep", body.ResultsKeep, 1, plans.MaxResultsKeep, &projects.ResultsKeep},
	} {
		if number.value == nil {
			continue
		}
		if *number.value < number.min || *number.value > number.max {
			return refuse(number.field, "invalid"), nil
		}
		*number.into = *number.value
	}
	if body.MaximumUploadBytes != nil {
		if *body.MaximumUploadBytes < minUploadBytes || *body.MaximumUploadBytes > maxUploadBytes {
			return refuse("maximum_upload_bytes", "invalid"), nil
		}
		server.MaximumUploadBytes = *body.MaximumUploadBytes
	}

	saved, err := s.store.SetServer(server, projects)
	if err != nil {
		return nil, err
	}
	s.index.SetCaps(plans.Caps{BackupsKeep: saved.Projects.BackupsKeep, ResultsKeep: saved.Projects.ResultsKeep})

	changed := changedSettings(before, saved)
	by, _ := callerID(ctx)
	logger.Log.Info(s99logger.NewEvent(logger.EventServerSettingsChanged,
		s99logger.String("changed", strings.Join(slices.Sorted(maps.Keys(changed)), ",")),
		s99logger.String("by", by)))
	if len(changed) > 0 {
		audited(ctx, s.trail, audit.SettingChanged, audit.Target{Kind: audit.Setting, ID: "server"}, changed)
	}
	return gen.UpdateServerSettings200JSONResponse(s.body(saved)), nil
}

// body is the stored settings on the wire.
func (s *ServerSettings) body(stored config.Settings) gen.ServerSettings {
	pending := []gen.ServerSettingName{}
	if stored.Server.MaximumUploadBytes != s.running.MaximumUploadBytes {
		pending = append(pending, gen.MaximumUploadBytes)
	}
	if stored.Server.RateLimitPerMinute != s.running.RateLimitPerMinute {
		pending = append(pending, gen.RateLimitPerMinute)
	}
	return gen.ServerSettings{
		PublicAddresses:    append([]string{}, stored.Server.PublicAddresses...),
		MaximumUploadBytes: stored.Server.MaximumUploadBytes,
		RateLimitPerMinute: stored.Server.RateLimitPerMinute,
		TrashDays:          stored.Projects.TrashDays,
		BackupsKeep:        stored.Projects.BackupsKeep,
		ResultsKeep:        stored.Projects.ResultsKeep,
		// The upload cap and the limiter are built into the router at start;
		// everything else is read where it is used.
		Applies: gen.ServerSettingsApplies{
			PublicAddresses:    gen.Now,
			MaximumUploadBytes: gen.Restart,
			RateLimitPerMinute: gen.Restart,
			TrashDays:          gen.Now,
			BackupsKeep:        gen.Now,
			ResultsKeep:        gen.Now,
		},
		RestartPending: pending,
	}
}

// cleanAddresses trims each address and checks it, returning the field and
// code of the first refusal.
func cleanAddresses(raw []string) ([]string, string, string) {
	if len(raw) > maxPublicAddresses {
		return nil, "public_addresses", "too_many"
	}
	out := make([]string, 0, len(raw))
	for i, value := range raw {
		address := strings.TrimSpace(value)
		field := fmt.Sprintf("public_addresses[%d]", i)
		if !validAddress(address) {
			return nil, field, "invalid"
		}
		if slices.Contains(out, address) {
			return nil, field, "duplicate"
		}
		out = append(out, address)
	}
	return out, "", ""
}

// validAddress is an absolute http or https URL with a host and no user,
// query or fragment.
func validAddress(address string) bool {
	if utf8.RuneCountInString(address) > maxAddressLength || !publicAddress.MatchString(address) {
		return false
	}
	parsed, err := url.Parse(address)
	return err == nil && parsed.Hostname() != ""
}

// changedSettings is each setting that differs between two stored states,
// with its new value; a list is comma-separated.
func changedSettings(before, after config.Settings) map[string]string {
	changed := map[string]string{}
	if !slices.Equal(before.Server.PublicAddresses, after.Server.PublicAddresses) {
		changed["public_addresses"] = strings.Join(after.Server.PublicAddresses, ",")
	}
	if before.Server.MaximumUploadBytes != after.Server.MaximumUploadBytes {
		changed["maximum_upload_bytes"] = strconv.FormatInt(after.Server.MaximumUploadBytes, 10)
	}
	for _, number := range []struct {
		name          string
		before, after int
	}{
		{"rate_limit_per_minute", before.Server.RateLimitPerMinute, after.Server.RateLimitPerMinute},
		{"trash_days", before.Projects.TrashDays, after.Projects.TrashDays},
		{"backups_keep", before.Projects.BackupsKeep, after.Projects.BackupsKeep},
		{"results_keep", before.Projects.ResultsKeep, after.Projects.ResultsKeep},
	} {
		if number.before != number.after {
			changed[number.name] = strconv.Itoa(number.after)
		}
	}
	return changed
}
