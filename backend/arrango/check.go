// arrango/check.go

package arrango

import (
	"context"

	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

// Checker asks the first paired, unrevoked node what a solve's input checks say.
type Checker struct {
	nodes  *Registry
	dialer *solveclient.Dialer
}

// NewChecker binds the registry and the dialer; with either nil every check is ErrNoNode.
func NewChecker(nodes *Registry, dialer *solveclient.Dialer) *Checker {
	return &Checker{nodes: nodes, dialer: dialer}
}

// Check runs one Check on its own connection, after GetNodeInfo confirms the node can.
func (c *Checker) Check(ctx context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error) {
	if c == nil || c.nodes == nil || c.dialer == nil {
		return nil, ErrNoNode
	}
	conn, err := dialFirst(ctx, c.nodes, c.dialer)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.Check(ctx, request)
}

// dialFirst connects to the node paired earliest that is not revoked; the caller closes the connection.
func dialFirst(ctx context.Context, nodes *Registry, dialer *solveclient.Dialer) (*solveclient.Conn, error) {
	node, err := nodes.First(ctx)
	if err != nil {
		return nil, err
	}
	return dialer.Reach(ctx, node.Label, node.Address, node.Fingerprint)
}
