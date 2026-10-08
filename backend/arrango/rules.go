// arrango/rules.go

package arrango

import (
	"context"

	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

// Rules asks the first paired, unrevoked node for the solver's rule table and built-in profiles.
type Rules struct {
	nodes  *Registry
	dialer *solveclient.Dialer
}

// NewRules binds the registry and the dialer; with either nil every call is ErrNoNode.
func NewRules(nodes *Registry, dialer *solveclient.Dialer) *Rules {
	return &Rules{nodes: nodes, dialer: dialer}
}

// Catalogue runs one GetRuleCatalogue on its own connection, after GetNodeInfo confirms the node can.
func (r *Rules) Catalogue(ctx context.Context) (*arrangorpcv1.RuleCatalogue, error) {
	if r == nil || r.nodes == nil || r.dialer == nil {
		return nil, ErrNoNode
	}
	conn, err := dialFirst(ctx, r.nodes, r.dialer)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.RuleCatalogue(ctx)
}
