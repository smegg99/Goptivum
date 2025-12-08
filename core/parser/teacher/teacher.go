// core/parser/teacher/teacher.go
package teacher

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"smegg.me/goptivum/core/models"
	"smegg.me/goptivum/core/parser/common"
	"smegg.me/goptivum/core/parser/schedule"
)

type Parser struct {
	Teacher *models.Teacher
}

func New() *Parser {
	return &Parser{
		Teacher: &models.Teacher{},
	}
}

func (tp *Parser) Parse(r io.Reader) error {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return fmt.Errorf("failed to parse HTML: %w", err)
	}

	titleText := doc.Find(".tytulnapis").Text()
	if titleText == "" {
		return fmt.Errorf("teacher title not found")
	}

	name, designator, err := parseTitle(titleText)
	if err != nil {
		return err
	}

	tp.Teacher.Name = name
	tp.Teacher.Designator = designator

	sched, err := schedule.ParseSchedule(doc, parseLesson)
	if err != nil {
		return err
	}

	tp.Teacher.Schedule = sched
	return nil
}

func parseTitle(title string) (name, designator string, err error) {
	re := regexp.MustCompile(`^(.+?)\s+\(([^)]+)\)$`)
	matches := re.FindStringSubmatch(strings.TrimSpace(title))
	if len(matches) != 3 {
		return "", "", fmt.Errorf("invalid teacher title format: %s", title)
	}
	return matches[1], matches[2], nil
}

func parseLesson(cell *goquery.Selection) []models.Lesson {
	html, _ := cell.Html()
	if strings.TrimSpace(html) == "&nbsp;" || strings.TrimSpace(html) == "" {
		return []models.Lesson{{}}
	}

	var lessons []models.Lesson
	lessonBlocks := strings.Split(html, "<br/>")

	for _, block := range lessonBlocks {
		if strings.TrimSpace(block) == "" {
			continue
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(block))
		if err != nil {
			continue
		}

		lesson := models.Lesson{}

		divisionLink := doc.Find("a.o")
		if divisionLink.Length() > 0 {
			divisionText := strings.TrimSpace(divisionLink.Text())
			lesson.DivisionDesignator = common.ExtractDesignator(divisionText)
			lesson.GroupType = common.ExtractGroupType(divisionText)
		}

		lessonName := doc.Find("span.p")
		if lessonName.Length() > 0 {
			lesson.Name = strings.TrimSpace(lessonName.Text())
		}

		classroomLink := doc.Find("a.s")
		if classroomLink.Length() > 0 {
			lesson.ClassroomDesignator = strings.TrimSpace(classroomLink.Text())
		}

		if lesson.Name != "" {
			lessons = append(lessons, lesson)
		}
	}

	if len(lessons) == 0 {
		return []models.Lesson{{}}
	}

	return lessons
}
