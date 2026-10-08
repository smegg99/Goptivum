// arrango/demo.go

package arrango

import (
	"context"
	"fmt"
	"time"

	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// demoTimeout bounds asking a node for the demo school. The node builds it in a
// child process in about a second; the bound stays well under the server's
// default 30 second write timeout, so a slow node is an answer and not a cut
// connection.
const demoTimeout = 20 * time.Second

// DemoSchool asks the first paired, unrevoked node for Arrango's synthetic
// production-sized school (about 1272 lessons). ErrNoNode means no node can be
// asked.
func DemoSchool(ctx context.Context, nodes *Registry, dialer *solveclient.Dialer) (*arrangov1.SchoolModel, error) {
	if nodes == nil || dialer == nil {
		return nil, ErrNoNode
	}
	node, err := nodes.First(ctx)
	if err != nil {
		return nil, err
	}

	askCtx, cancel := context.WithTimeout(ctx, demoTimeout)
	defer cancel()
	conn, err := dialer.Reach(askCtx, node.Label, node.Address, node.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("arrango: reach %s for the demo school: %w", node.Label, err)
	}
	defer conn.Close()

	model, err := conn.Solver.GetDemoSchool(askCtx, &arrangorpcv1.DemoRequest{
		Preset: arrangorpcv1.DemoPreset_DEMO_PRESET_PRODUCTION,
		Seed:   0,
	})
	if err != nil {
		return nil, fmt.Errorf("arrango: demo school from %s: %w", node.Label, err)
	}
	return model, nil
}
