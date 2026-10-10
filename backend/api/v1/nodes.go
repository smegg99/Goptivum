// api/v1/nodes.go

package v1

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/solveclient"
)

// maxNodeLabel is the contract's maxLength for a node's name, in characters.
const maxNodeLabel = 64

// Nodes is what the node routes need. With no Registry every node route
// answers 404; with no Dialer pairing does too and a status says unreachable;
// with no Runner a revoke has no solves to stop; with no Plans a node's history
// names no projects.
type Nodes struct {
	Registry *arrango.Registry
	Dialer   *solveclient.Dialer
	Runner   *arrango.Runner
	Jobs     *arrango.Jobs
	Plans    *plans.Repository
}

// ListNodes is every paired node, revoked ones included.
func (a *Admin) ListNodes(ctx context.Context, _ gen.ListNodesRequestObject) (gen.ListNodesResponseObject, error) {
	if a.nodes.Registry == nil {
		return gen.ListNodes404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	nodes, err := a.nodes.Registry.List(ctx)
	if err != nil {
		return nil, err
	}
	items := []gen.Node{}
	for _, node := range nodes {
		items = append(items, toNode(node))
	}
	return gen.ListNodes200JSONResponse{Items: items}, nil
}

// PairNode redeems a pairing URI.
func (a *Admin) PairNode(ctx context.Context, request gen.PairNodeRequestObject) (gen.PairNodeResponseObject, error) {
	if a.nodes.Registry == nil || a.nodes.Dialer == nil {
		return gen.PairNode404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	body := request.Body
	if body == nil || strings.TrimSpace(body.Uri) == "" {
		return gen.PairNode422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("uri", "required"))}, nil
	}
	label := ""
	if body.Label != nil {
		label = strings.TrimSpace(*body.Label)
		if utf8.RuneCountInString(label) > maxNodeLabel {
			return gen.PairNode422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("label", "too_long"))}, nil
		}
	}

	// The token travels only after the presented certificate matched the pin,
	// so a bad URI and an impostor are both refused before anything is sent.
	paired, err := a.nodes.Dialer.Pair(ctx, body.Uri)
	if err != nil {
		logger.Log.Warn(s99logger.NewEvent(logger.EventNodePairingRefused, s99logger.Err(err)))
		return gen.PairNode422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(
			gen.Error{Code: "validation", Message: pairingRefusal(err)})}, nil
	}

	if label == "" {
		// A node paired again keeps its name; a new one is called "node".
		_, err := a.nodes.Registry.ByFingerprint(ctx, paired.Fingerprint.String())
		switch {
		case errors.Is(err, arrango.ErrNoNode):
			label = "node"
		case err != nil:
			return nil, err
		}
	}
	node, err := a.nodes.Registry.Record(ctx, label, paired)
	if err != nil {
		return nil, err
	}
	audited(ctx, a.trail, audit.NodePaired, nodeTarget(node), map[string]string{"address": node.Address})
	return gen.PairNode201JSONResponse{
		Node:             toNode(node),
		NodeApiVersion:   paired.NodeAPI,
		SolverVersion:    paired.Solver,
		MinClientVersion: paired.MinClient,
	}, nil
}

// pairingRefusal is the key that says why pairing failed.
func pairingRefusal(err error) string {
	switch {
	case errors.Is(err, solveclient.ErrNotALink):
		return messages.KeyNodeNotALink
	case errors.Is(err, solveclient.ErrPinMismatch):
		return messages.KeyNodeFingerprintMismatch
	case errors.Is(err, solveclient.ErrLinkSpent):
		return messages.KeyNodeLinkSpent
	case errors.Is(err, solveclient.ErrClientTooOld):
		return messages.KeyNodeClientTooOld
	case errors.Is(err, solveclient.ErrUnreachable):
		return messages.KeyNodeUnreachable
	default:
		return messages.KeyNodePairingFailed
	}
}

// GetNode reads one node.
func (a *Admin) GetNode(ctx context.Context, request gen.GetNodeRequestObject) (gen.GetNodeResponseObject, error) {
	node, found, err := a.node(ctx, request.NodeId)
	if err != nil {
		return nil, err
	}
	if !found {
		return gen.GetNode404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	return gen.GetNode200JSONResponse(toNode(node)), nil
}

// UpdateNode renames a node or sets this server's limits on it, a limit only
// within the node's own ceilings for this server as the node reports them now.
func (a *Admin) UpdateNode(ctx context.Context, request gen.UpdateNodeRequestObject) (gen.UpdateNodeResponseObject, error) {
	node, found, err := a.node(ctx, request.NodeId)
	if err != nil {
		return nil, err
	}
	if !found {
		return gen.UpdateNode404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	invalid := func(field, code string) gen.UpdateNodeResponseObject {
		return gen.UpdateNode422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(field, code))}
	}
	body := request.Body
	if body == nil || (body.Label == nil && body.SolvesAtOnce == nil && body.WorkersPerSolve == nil) {
		return invalid("body", "empty"), nil
	}

	var change arrango.NodeChange
	if body.Label != nil {
		label := strings.TrimSpace(*body.Label)
		switch {
		case label == "":
			return invalid("label", "required"), nil
		case utf8.RuneCountInString(label) > maxNodeLabel:
			return invalid("label", "too_long"), nil
		}
		change.Label = &label
	}
	if body.SolvesAtOnce != nil || body.WorkersPerSolve != nil {
		limits := node.Limits
		if body.SolvesAtOnce != nil {
			if *body.SolvesAtOnce < 1 || *body.SolvesAtOnce > arrango.MaxSolves {
				return invalid("solves_at_once", "invalid"), nil
			}
			limits.Solves = *body.SolvesAtOnce
		}
		if body.WorkersPerSolve != nil {
			if *body.WorkersPerSolve < 1 || *body.WorkersPerSolve > int(solveclient.MaxWorkers) {
				return invalid("workers_per_solve", "invalid"), nil
			}
			limits.Workers = *body.WorkersPerSolve
		}
		if refusal, ok := a.withinCeilings(ctx, node, limits); !ok {
			return refusal, nil
		}
		change.Solves, change.Workers = body.SolvesAtOnce, body.WorkersPerSolve
	}

	updated, err := a.nodes.Registry.Update(ctx, node.ID, change)
	if err != nil {
		return nil, err
	}
	changed := map[string]string{}
	if updated.Label != node.Label {
		changed["from"], changed["to"] = node.Label, updated.Label
	}
	if updated.Limits.Solves != node.Limits.Solves {
		changed["solves_at_once"] = strconv.Itoa(updated.Limits.Solves)
	}
	if updated.Limits.Workers != node.Limits.Workers {
		changed["workers_per_solve"] = strconv.Itoa(updated.Limits.Workers)
	}
	if len(changed) > 0 {
		audited(ctx, a.trail, audit.NodeChanged, nodeTarget(updated), changed)
	}
	return gen.UpdateNode200JSONResponse(toNode(updated)), nil
}

// withinCeilings asks the node for its ceilings for this server and refuses
// limits above them; the second value is false with the refusal to answer.
func (a *Admin) withinCeilings(ctx context.Context, node arrango.Node, limits arrango.Limits) (gen.UpdateNodeResponseObject, bool) {
	conflict := func(key string, details map[string]string) gen.UpdateNodeResponseObject {
		envelope := gen.Error{Code: "conflict", Message: key}
		if details != nil {
			envelope.Details = &details
		}
		return gen.UpdateNode409JSONResponse(envelope)
	}
	if node.Revoked() {
		return conflict(messages.KeyNodeRevoked, nil), false
	}
	if a.nodes.Dialer == nil {
		return conflict(messages.KeyNodeUnreachable, nil), false
	}
	probe, err := arrango.Probe(ctx, a.nodes.Dialer, node)
	if err != nil {
		return conflict(string(problemOf(err)), nil), false
	}
	if probe.Capacity.Ceilings == nil {
		return conflict(messages.KeyNodeOutdated, map[string]string{"Capability": solveclient.CapabilityPeerPolicy}), false
	}
	if err := limits.AllowedBy(*probe.Capacity.Ceilings); err != nil {
		envelope, _ := refused(err)
		return gen.UpdateNode422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(envelope)}, false
	}
	return nil, true
}

// RevokeNode stops this server dispatching to a node, then stops the node's
// solves and answers once their jobs are closed.
func (a *Admin) RevokeNode(ctx context.Context, request gen.RevokeNodeRequestObject) (gen.RevokeNodeResponseObject, error) {
	if a.nodes.Registry == nil {
		return gen.RevokeNode404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	node, found, err := a.node(ctx, request.NodeId)
	if err != nil {
		return nil, err
	}
	if !found {
		return gen.RevokeNode404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	id := node.ID
	if a.nodes.Runner != nil {
		err = a.nodes.Runner.Revoke(ctx, id)
	} else {
		err = a.nodes.Registry.Revoke(ctx, id)
	}
	switch {
	case errors.Is(err, arrango.ErrNoNode):
		return gen.RevokeNode404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	// A caller that went away while its solves stopped: the node is revoked
	// and the cancels went out whatever this wait says.
	case err != nil && ctx.Err() == nil:
		return nil, err
	}
	audited(ctx, a.trail, audit.NodeRevoked, nodeTarget(node), nil)
	return gen.RevokeNode204Response{}, nil
}

// GetNodeStatus asks the node now. A node that does not answer is a status,
// not an error; a revoked one is not asked.
func (a *Admin) GetNodeStatus(ctx context.Context, request gen.GetNodeStatusRequestObject) (gen.GetNodeStatusResponseObject, error) {
	node, found, err := a.node(ctx, request.NodeId)
	if err != nil {
		return nil, err
	}
	if !found {
		return gen.GetNodeStatus404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	out := gen.NodeStatus{NodeId: strconv.FormatInt(node.ID, 10)}
	if a.nodes.Jobs != nil {
		// This server's own solves there, not the node's count, which also
		// holds checks and other deployments' solves.
		if out.RunningSolves, err = a.nodes.Jobs.CountActive(ctx, node.ID); err != nil {
			return nil, err
		}
	}
	if node.Revoked() {
		return gen.GetNodeStatus200JSONResponse(out), nil
	}
	if a.nodes.Dialer == nil {
		out.Problem = pointerTo(gen.NodeUnreachable)
		return gen.GetNodeStatus200JSONResponse(out), nil
	}
	probe, err := arrango.Probe(ctx, a.nodes.Dialer, node)
	if err != nil {
		out.Problem = pointerTo(problemOf(err))
		return gen.GetNodeStatus200JSONResponse(out), nil
	}

	capacity := probe.Capacity
	out.Reachable = true
	out.NodeApiVersion = pointerTo(probe.NodeAPI)
	out.SolverVersion = pointerTo(probe.Solver)
	out.MinClientVersion = pointerTo(probe.MinClient)
	out.Cores = pointerTo(int(capacity.Cores))
	out.FreeMemoryBytes = pointerTo(int64(capacity.FreeBytes))
	if ceilings := capacity.Ceilings; ceilings != nil {
		out.Ceilings = &gen.NodeCeilings{WorkersPerSolve: ceilings.Workers, MaxSolveSeconds: ceilings.Seconds}
		if ceilings.Solves > 0 {
			out.Ceilings.SolvesAtOnce = pointerTo(ceilings.Solves)
		}
	}
	return gen.GetNodeStatus200JSONResponse(out), nil
}

// problemOf is why a node that was asked did not answer, as the status names it.
func problemOf(err error) gen.NodeStatusProblem {
	switch {
	case errors.Is(err, solveclient.ErrPinMismatch):
		return gen.NodeFingerprintMismatch
	case errors.Is(err, solveclient.ErrNotPaired):
		return gen.NodeNotPaired
	case errors.Is(err, solveclient.ErrClientTooOld):
		return gen.NodeClientTooOld
	default:
		return gen.NodeUnreachable
	}
}

// ListNodeJobs is one page of the solves this server ran on a node, whoever
// started them, projects in Trash included.
func (a *Admin) ListNodeJobs(ctx context.Context, request gen.ListNodeJobsRequestObject) (gen.ListNodeJobsResponseObject, error) {
	node, found, err := a.node(ctx, request.NodeId)
	if err != nil {
		return nil, err
	}
	if !found || a.nodes.Jobs == nil {
		return gen.ListNodeJobs404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	page, limit := pageOf(request.Params.Page, request.Params.Limit)
	rows, total, err := a.nodes.Jobs.ForNode(ctx, node.ID, (page-1)*limit, limit)
	if err != nil {
		return nil, err
	}
	projects := map[int64]plans.Plan{}
	if a.nodes.Plans != nil {
		if projects, err = a.nodes.Plans.Lookup(ctx, planIDsOf(rows)); err != nil {
			return nil, err
		}
	}
	caller, _ := callerID(ctx)
	items := make([]gen.Job, 0, len(rows))
	for _, row := range rows {
		items = append(items, toJob(row, projects[row.PlanID], caller))
	}
	return gen.ListNodeJobs200JSONResponse{Items: items, Page: page, Limit: limit, Total: total}, nil
}

// node resolves a node id from a path. found is false for an id that is not a
// number, a node that does not exist, or no registry at all.
func (a *Admin) node(ctx context.Context, raw string) (arrango.Node, bool, error) {
	if a.nodes.Registry == nil {
		return arrango.Node{}, false, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return arrango.Node{}, false, nil
	}
	node, err := a.nodes.Registry.Get(ctx, id)
	if errors.Is(err, arrango.ErrNoNode) {
		return arrango.Node{}, false, nil
	}
	if err != nil {
		return arrango.Node{}, false, err
	}
	return node, true, nil
}

// nodeTarget is a node as the audit trail names it.
func nodeTarget(node arrango.Node) audit.Target {
	return audit.Target{Kind: audit.Node, ID: strconv.FormatInt(node.ID, 10), Name: node.Label}
}

func toNode(node arrango.Node) gen.Node {
	out := gen.Node{
		Id:              fmt.Sprintf("%d", node.ID),
		Label:           node.Label,
		Address:         node.Address,
		Fingerprint:     node.Fingerprint,
		PairedAt:        node.PairedAt,
		Active:          !node.Revoked(),
		SolvesAtOnce:    node.Limits.Solves,
		WorkersPerSolve: node.Limits.Workers,
	}
	if node.RevokedAt != nil {
		revoked := *node.RevokedAt
		out.RevokedAt = &revoked
	}
	return out
}
