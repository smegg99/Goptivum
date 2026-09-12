// formats/detect.go

package formats

import (
	"errors"
	"sort"
)

// Match is one importer's claim on a Source.
type Match struct {
	ID         string
	Confidence Confidence
	Importer   Importer
}

// Detect asks every registered importer what it makes of the Source and
// returns the claims ranked by confidence, best first. Detection is by
// content: a ".zip" could be anything and a ".pla" is recognisable by its
// own header.
//
// Nothing claiming the Source is an empty slice and a nil error, not an
// error. An error comes back only when every importer failed to look.
func (r *Registry) Detect(src Source) ([]Match, error) {
	importers := r.Importers()

	var matches []Match
	var failures []error
	for _, imp := range importers {
		confidence, err := imp.Detect(src)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if confidence == No {
			continue
		}
		matches = append(matches, Match{ID: imp.ID(), Confidence: confidence, Importer: imp})
	}
	if len(matches) == 0 && len(failures) == len(importers) && len(failures) > 0 {
		return nil, errors.Join(failures...)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Confidence != matches[j].Confidence {
			return matches[i].Confidence > matches[j].Confidence
		}
		return matches[i].ID < matches[j].ID
	})
	return matches, nil
}

// Best returns the single importer to use for a Source.
func (r *Registry) Best(src Source) (Importer, error) {
	matches, err := r.Detect(src)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, ErrUnrecognised
	}
	return matches[0].Importer, nil
}

// ErrUnrecognised means no importer claimed the Source.
var ErrUnrecognised = errors.New("formats: no importer recognises this file")
