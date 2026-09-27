// solveclient/profile.go

package solveclient

import (
	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// Profile is a rule profile a solve uses in place of the plan's own rules.
type Profile struct {
	// Name is a built-in's name, or a school profile's name when the solve started.
	Name string
	// ID is the school profile's id, 0 for a built-in.
	ID int64
	// Rules is the request's rule_config: a built-in by name, or a school profile's overrides.
	Rules *arrangov1.RuleConfig
}

// Apply sends the profile as the request's rules and the model without rules
// of its own: a request's rules are layered over the model's and cannot reset
// a weight or param the model set, so leaving them would mix the two.
func (p *Profile) Apply(request *arrangorpcv1.SolveRequest) {
	if request.GetModel() != nil {
		request.Model.RuleConfig = nil
	}
	request.RuleConfig = p.Rules
}
