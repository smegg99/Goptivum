// core/parser/schedule/schedule.go
package schedule

import (
	"github.com/PuerkitoBio/goquery"
	"smegg.me/goptivum/core/models"
)

type LessonParser func(*goquery.Selection) []models.Lesson

func ParseSchedule(doc *goquery.Document, parser LessonParser) (models.Schedule, error) {
	schedule := models.Schedule{}

	rows := doc.Find("table.tabela tr")
	rows.Each(func(i int, row *goquery.Selection) {
		if i == 0 {
			return
		}

		cells := row.Find("td.l")
		dayIndex := 0

		cells.Each(func(j int, cell *goquery.Selection) {
			lessons := parser(cell)

			switch dayIndex {
			case 0:
				schedule.Monday = append(schedule.Monday, lessons...)
			case 1:
				schedule.Tuesday = append(schedule.Tuesday, lessons...)
			case 2:
				schedule.Wednesday = append(schedule.Wednesday, lessons...)
			case 3:
				schedule.Thursday = append(schedule.Thursday, lessons...)
			case 4:
				schedule.Friday = append(schedule.Friday, lessons...)
			}

			dayIndex++
		})
	})

	return schedule, nil
}
