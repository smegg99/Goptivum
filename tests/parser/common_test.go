// tests/parser/common_test.go
package parser_test

import (
	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/models"
)

const resourcesDir = "../../resources/plany"

func init() {
	logger.Configure(logger.Config{
		Level:       "DEBUG",
		NoColor:     false,
		EnableFiles: false,
		Prefix:      "TEST",
	})
}

func countScheduleLessons(schedule models.Schedule) int {
	count := 0
	count += countNonEmptyLessons(schedule.Monday)
	count += countNonEmptyLessons(schedule.Tuesday)
	count += countNonEmptyLessons(schedule.Wednesday)
	count += countNonEmptyLessons(schedule.Thursday)
	count += countNonEmptyLessons(schedule.Friday)
	return count
}

func countNonEmptyLessons(lessons []models.Lesson) int {
	count := 0
	for _, l := range lessons {
		if l.Name != "" {
			count++
		}
	}
	return count
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
