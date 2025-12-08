// core/parser/common/common.go
package common

import (
	"io"
	"regexp"
	"strings"

	"smegg.me/goptivum/core/models"
)

type Parser interface {
	Parse(r io.Reader) error
}

func ExtractGroupType(text string) models.GroupType {
	if strings.Contains(text, "1/3") {
		return models.Group1
	}
	if strings.Contains(text, "2/3") {
		return models.Group2
	}
	if strings.Contains(text, "3/3") {
		return models.Group3
	}
	return models.NoGroup
}

func ExtractDesignator(text string) string {
	parts := strings.Split(text, "-")
	return strings.TrimSpace(parts[0])
}

func ExtractLessonName(text string) string {
	re := regexp.MustCompile(`^(.+?)(?:-\d+/\d+)?$`)
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return strings.TrimSpace(text)
}
