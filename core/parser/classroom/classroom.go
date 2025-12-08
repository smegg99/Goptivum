// core/parser/classroom/classroom.go
package classroom

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
	Classroom *models.Classroom
}

func New() *Parser {
	return &Parser{
		Classroom: &models.Classroom{},
	}
}

func (cp *Parser) Parse(r io.Reader) error {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return fmt.Errorf("failed to parse HTML: %w", err)
	}

	titleText := doc.Find(".tytulnapis").Text()
	if titleText == "" {
		return fmt.Errorf("classroom title not found")
	}

	name, designator := parseTitle(titleText)
	cp.Classroom.Name = name
	cp.Classroom.Designator = designator

	sched, err := schedule.ParseSchedule(doc, parseLesson)
	if err != nil {
		return err
	}

	cp.Classroom.Schedule = sched
	return nil
}

func parseTitle(title string) (name, designator string) {
	parts := strings.Fields(strings.TrimSpace(title))
	if len(parts) > 0 {
		designator = parts[0]
	}
	name = strings.TrimSpace(title)
	return name, designator
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

		teacherLink := doc.Find("a.n")
		if teacherLink.Length() > 0 {
			lesson.TeacherDesignator = strings.TrimSpace(teacherLink.Text())
		}

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

		if lesson.Name != "" {
			lessons = append(lessons, lesson)
		}
	}

	if len(lessons) == 0 {
		return []models.Lesson{{}}
	}

	return lessons
}
