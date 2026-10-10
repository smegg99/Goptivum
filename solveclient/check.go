// solveclient/check.go

package solveclient

import (
	"context"
	"slices"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

// The node's names, in GetNodeInfo, for the RPCs a build may lack.
const (
	CapabilityCheck         = "check"
	CapabilityRuleCatalogue = "rule_catalogue"
	CapabilityRuleMetadata  = "rule_metadata"
)

// Capable confirms through GetNodeInfo that the node's build has a capability;
// one that lacks it is an *ErrOutdated.
func (c *Conn) Capable(ctx context.Context, capabilities ...string) error {
	info, err := c.Node.GetNodeInfo(ctx, &arrangorpcv1.NodeInfoRequest{})
	if err != nil {
		return callError(c.label, "node info", err)
	}
	for _, capability := range capabilities {
		if !slices.Contains(info.GetCapabilities(), capability) {
			return &ErrOutdated{Node: c.label, Capability: capability}
		}
	}
	return nil
}

// Check runs the solver's input checks over a model and, when the request
// carries one, its timetable. A node without the capability is *ErrOutdated.
func (c *Conn) Check(ctx context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error) {
	if err := c.Capable(ctx, CapabilityCheck); err != nil {
		return nil, err
	}
	report, err := c.Solver.Check(ctx, request)
	if err != nil {
		return nil, callError(c.label, "check", err)
	}
	return report, nil
}

// RuleCatalogue is the solver's rule table and built-in profiles. A node
// without the capability is *ErrOutdated.
func (c *Conn) RuleCatalogue(ctx context.Context) (*arrangorpcv1.RuleCatalogue, error) {
	if err := c.Capable(ctx, CapabilityRuleCatalogue, CapabilityRuleMetadata); err != nil {
		return nil, err
	}
	catalogue, err := c.Solver.GetRuleCatalogue(ctx, &arrangorpcv1.RuleCatalogueRequest{})
	if err != nil {
		return nil, callError(c.label, "rule catalogue", err)
	}
	return catalogue, nil
}
