// api/v1/school.go

package v1

import (
	"context"
	"errors"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/escrow"
)

// School is what belongs to the school rather than to one person.
//
// One key for the whole school was the decision, so these routes need a session
// and no role: a planner who cannot open the school's files cannot do their
// work.
type School struct {
	keys *escrow.Store
}

// NewSchool wires the key store. A nil store is a deployment that holds no key,
// which answers honestly rather than failing.
func NewSchool(keys *escrow.Store) *School { return &School{keys: keys} }

// holds reports whether this deployment has a key to talk about.
//
// Nil-receiver safe on purpose: a Handlers built without the school half is a
// server with no escrow, and a route that panics is a worse answer than one
// that says there is no key.
func (s *School) holds() bool { return s != nil && s.keys != nil && s.keys.Enabled() }

// GetSchoolKey is the public half, which is not a secret. It is read without
// unlocking the key, so it costs no scrypt.
func (s *School) GetSchoolKey(ctx context.Context, _ gen.GetSchoolKeyRequestObject) (gen.GetSchoolKeyResponseObject, error) {
	if !s.holds() {
		return gen.GetSchoolKey200JSONResponse{Configured: false}, nil
	}
	public, err := s.keys.Public(ctx)
	if errors.Is(err, escrow.ErrNoKey) {
		return gen.GetSchoolKey200JSONResponse{Configured: false}, nil
	}
	if err != nil {
		return nil, err
	}
	recipient, created := public.Recipient, public.CreatedAt
	return gen.GetSchoolKey200JSONResponse{
		Configured: true, Recipient: &recipient, CreatedAt: &created,
	}, nil
}

// ReleaseSchoolKey hands the private half to a signed-in member of the school.
//
// This returns the secret that opens every file the school has encrypted. It is
// a POST so it never sits in a cacheable response, and it is the whole reason
// the key is escrowed: a desktop that holds it opens a .gpf without sending the
// file to the server.
func (s *School) ReleaseSchoolKey(ctx context.Context, _ gen.ReleaseSchoolKeyRequestObject) (gen.ReleaseSchoolKeyResponseObject, error) {
	if !s.holds() {
		return gen.ReleaseSchoolKey404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	key, err := s.keys.Get(ctx)
	if errors.Is(err, escrow.ErrNoKey) {
		return gen.ReleaseSchoolKey404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if err != nil {
		// A key the escrow passphrase no longer opens, or a database that
		// did not answer: either way there is no key to hand out now.
		return gen.ReleaseSchoolKey503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.ReleaseSchoolKey200JSONResponse{
		Identity: key.Identity.String(), Recipient: key.Recipient,
	}, nil
}
