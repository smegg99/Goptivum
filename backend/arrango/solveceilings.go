package arrango

import (
	"context"
	"sync"
	"time"

	"github.com/smegg99/goptivum/solveclient"
)

// MaxSolveSeconds reports the largest answering node ceiling within one second.
// A failed dependency contributes no ceiling; discovery itself stays available.
func MaxSolveSeconds(ctx context.Context, nodes *Registry, dialer *solveclient.Dialer) float64 {
	if nodes == nil || dialer == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	active, err := nodes.Active(ctx)
	if err != nil {
		return 0
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var maximum float64
	for _, node := range active {
		wg.Go(func() {
			probe, err := Probe(ctx, dialer, node)
			if err != nil || probe.Capacity.Ceilings == nil {
				return
			}
			mu.Lock()
			maximum = max(maximum, probe.Capacity.Ceilings.Seconds)
			mu.Unlock()
		})
	}
	wg.Wait()
	return maximum
}
