// formats/findings.go

package formats

import (
	"fmt"
	"strings"
)

// Merge folds findings a person would read as the same (kind, severity and
// entities; Detail is never read) into one, adding their counts, in the order
// each was first seen.
func Merge(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	at := map[string]int{}
	for _, finding := range findings {
		count := max(finding.Count, 1)
		key := identity(finding)
		if i, ok := at[key]; ok {
			out[i].Count += count
			continue
		}
		at[key] = len(out)
		finding.Count = count
		out = append(out, finding)
	}
	return out
}

// identity is what makes two findings the same fact.
func identity(finding Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d", finding.Kind, finding.Severity)
	for _, ref := range finding.Entities {
		fmt.Fprintf(&b, "|%d:%d", ref.GetKind(), ref.GetId())
	}
	return b.String()
}
