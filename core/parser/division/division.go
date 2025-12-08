// core/parser/division/division.go
package division

import (
	"fmt"
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"smegg.me/goptivum/core/models"
	"smegg.me/goptivum/core/parser/common"
	"smegg.me/goptivum/core/parser/schedule"
)

type Parser struct {
	Division *models.Division
}

func New() *Parser {
	return &Parser{
		Division: &models.Division{},
	}
}

func (dp *Parser) Parse(r io.Reader) error {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return fmt.Errorf("failed to parse HTML: %w", err)
	}

	titleText := doc.Find(".tytulnapis").Text()
	if titleText == "" {
		return fmt.Errorf("division title not found")
	}

	name, designator, err := parseTitle(titleText)
	if err != nil {
		return err
	}

	dp.Division.Name = name
	dp.Division.Designator = designator

	sched, err := schedule.ParseSchedule(doc, parseLesson)
	if err != nil {
		return err
	}

	dp.Division.Schedule = sched
	return nil
}

func parseTitle(title string) (name, designator string, err error) {
	parts := strings.Fields(strings.TrimSpace(title))
	if len(parts) < 1 {
		return "", "", fmt.Errorf("invalid division title format: %s", title)
	}
	designator = parts[0]
	name = strings.TrimSpace(title)
	return name, designator, nil
}

func parseLesson(cell *goquery.Selection) []models.Lesson {
	html, _ := cell.Html()
	if strings.TrimSpace(html) == "&nbsp;" || strings.TrimSpace(html) == "" {
		return []models.Lesson{{}}
	}

	text := strings.TrimSpace(cell.Text())
	if strings.Contains(text, "Zajęcia w CKZ") || strings.Contains(text, "CKZ") {
		return []models.Lesson{{Name: text}}
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

		lessonName := doc.Find("span.p")
		if lessonName.Length() > 0 {
			fullName := strings.TrimSpace(lessonName.Text())
			lesson.Name = common.ExtractLessonName(fullName)
			lesson.GroupType = common.ExtractGroupType(fullName)
		}

		teacherLink := doc.Find("a.n")
		if teacherLink.Length() > 0 {
			lesson.TeacherDesignator = strings.TrimSpace(teacherLink.Text())
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
