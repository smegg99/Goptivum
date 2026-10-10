// core/plans/summary.go

package plans

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// Summary counts what a version changed from its parent: per edit kind, then
// per kind of thing, such as {"added": {"teacher": 3}}. It is never prose, so
// every reader words it in its own language.
type Summary map[string]map[string]int

// The limits the contract states for a summary.
const (
	MaxSummaryBytes = 4096
	MaxSummaryCount = 1_000_000
)

// summaryName is an edit kind or a kind of thing: lower camel case, short.
var summaryName = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,31}$`)

// ParseSummary reads a summary from JSON and refuses anything else with an
// *Invalid. Empty kinds are dropped, and a summary with nothing left is nil.
func ParseSummary(data []byte) (Summary, error) {
	refused := &Invalid{Field: "summary", Code: "invalid"}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > MaxSummaryBytes {
		return nil, refused
	}
	var parsed Summary
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, refused
	}
	out := Summary{}
	for kind, counts := range parsed {
		if !summaryName.MatchString(kind) {
			return nil, refused
		}
		for thing, count := range counts {
			if !summaryName.MatchString(thing) || count < 1 || count > MaxSummaryCount {
				return nil, refused
			}
		}
		if len(counts) > 0 {
			out[kind] = counts
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// summaryText is how a summary is stored: canonical JSON with sorted keys, or
// null when there is none. A summary built in code is checked like one read
// from a request, and the summary returned is the one stored.
func summaryText(summary Summary) (Summary, any, error) {
	if len(summary) == 0 {
		return nil, nil, nil
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return nil, nil, &Invalid{Field: "summary", Code: "invalid"}
	}
	normal, err := ParseSummary(data)
	if err != nil || normal == nil {
		return nil, nil, err
	}
	if data, err = json.Marshal(normal); err != nil {
		return nil, nil, &Invalid{Field: "summary", Code: "invalid"}
	}
	return normal, string(data), nil
}
