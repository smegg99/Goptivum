// arrango/probe.go

package arrango

import (
	"context"

	"github.com/smegg99/goptivum/solveclient"
)

// Probe asks one node GetNodeInfo and GetNodeStatus within statusTimeout, the
// bound admission uses, so a node the Nodes page calls reachable is one a
// solve is admitted on. A failure is solveclient.ErrUnreachable (with
// ErrPinMismatch for another certificate), ErrNotPaired or ErrClientTooOld.
func Probe(ctx context.Context, dialer *solveclient.Dialer, node Node) (solveclient.Probe, error) {
	askCtx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	conn, err := dialer.Reach(askCtx, node.Label, node.Address, node.Fingerprint)
	if err != nil {
		return solveclient.Probe{}, err
	}
	defer conn.Close()
	return conn.Probe(askCtx)
}
