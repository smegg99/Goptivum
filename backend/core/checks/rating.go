// backend/core/checks/rating.go
package checks

import (
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/goptivum/solveclient"
)

func capRatings(score *Score, findings []Finding, model *arrangov1.SchoolModel, errors int) {
	classes, teachers := map[uint32]int{}, map[uint32]int{}
	index := solveclient.NewFindingIndex(model)
	for _, finding := range findings {
		if finding.Ignored || finding.Severity != SeverityError {
			continue
		}
		refs := map[string][]uint32{}
		for _, entity := range finding.Entities {
			refs[entity.Kind] = append(refs[entity.Kind], entity.ID)
		}

		lessons := make([]uint32, 0, len(finding.Lessons))
		for _, lesson := range finding.Lessons {
			lessons = append(lessons, lesson.ID)
		}
		a, b := index.Entities(refs, lessons)
		for id := range a {
			classes[id]++
		}
		for id := range b {
			teachers[id]++
		}
	}
	score.Rating = solveclient.Rating(score.Quality, errors)
	for n := range score.Classes {
		entity := &score.Classes[n]
		entity.Rating = solveclient.Rating(entity.Quality, classes[entity.ID])
	}
	for n := range score.Teachers {
		entity := &score.Teachers[n]
		entity.Rating = solveclient.Rating(entity.Quality, teachers[entity.ID])
	}
}
