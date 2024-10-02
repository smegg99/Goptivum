// scraper.go
package scraper

import (
	"fmt"
	"net/http"
	"strings"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/common/utils"

	"github.com/PuerkitoBio/goquery"
)

var GeneralConfig config.GeneralConfig
var Config config.ScraperConfig

func splitDivisionTitle(s string) (string, string) {
	parts := strings.Split(s, " ")
	if len(parts) < 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func parseTimeRange(s string) (TimeRange, error) {
	s = strings.ReplaceAll(s, " ", "")
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return TimeRange{}, fmt.Errorf("invalid time range: %s", s)
	}

	start, err := TimeString(parts[0]).ToTimestamp()
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid start time: %w", err)
	}

	end, err := TimeString(parts[1]).ToTimestamp()
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid end time: %w", err)
	}

	return TimeRange{Start: start, End: end}, nil
}

func parseLesson(rowElement *goquery.Selection, timeRange TimeRange) (Lesson, error) {
	lessonName := rowElement.Find("span.p").First().Text()
	teacher := rowElement.Find("a.n").First().Text()
	room := rowElement.Find("a.s").First().Text()
	lesson := Lesson{
		TimeRange: timeRange,
		FullName:  lessonName,
		Teacher:   teacher,
		Room:      room,
	}
	return lesson, nil
}

func parseLessons(rowElement *goquery.Selection, timeRange TimeRange) ([]Lesson, error) {
	lessons := []Lesson{}
	innerSpanElements := rowElement.Find("span > span.p")
	if innerSpanElements.Length() > 1 {
		innerSpanElements.Each(func(i int, s *goquery.Selection) {
			parentSelection := s.Parent()
			lesson, err := parseLesson(parentSelection, timeRange)
			if err != nil {
				fmt.Println("error parsing lesson", err)
				return
			}
			lessons = append(lessons, lesson)
		})
	} else {
		lesson, err := parseLesson(rowElement, timeRange)
		if err != nil {
			fmt.Println("error parsing lesson", err)
			return nil, err
		}
		lessons = append(lessons, lesson)
	}

	return lessons, nil
}

func scrapeTitle(doc *goquery.Document) (string, string, error) {
	titleSelection := doc.Find("body > table > tbody > tr > td").First()
	if titleSelection.Length() == 0 {
		return "", "", fmt.Errorf("no division title found")
	}

	title := titleSelection.Text()
	designator, fullName := splitDivisionTitle(title)

	return designator, fullName, nil
}

func scrapeSchedule(doc *goquery.Document) (Schedule, error) {
	var schedule Schedule
	var timeRange TimeRange

	rowsSelection := doc.Find("table.tabela > tbody > tr")
	firstRow := rowsSelection.First()
	if firstRow.Length() == 0 {
		return schedule, fmt.Errorf("no rows found")
	}

	columnNumber := 0
	columnsCount := firstRow.Children().Length()

	rowsLength := doc.Find("table.tabela > tbody > tr > td.nr").Length() + 1
	lessonsLength := doc.Find("table.tabela > tbody > tr > td.l").Length()
	
	// First row is the table headers row so it doesn't count
	scheduleStartColumn := columnsCount - (lessonsLength / (rowsLength - 1))

	doc.Find("table.tabela > tbody > tr > td").Each(func(i int, rowElement *goquery.Selection) {
		if columnNumber >= columnsCount {
			fmt.Println()
			columnNumber = 1
		} else {
			columnNumber++
		}
		fmt.Println("column:", columnNumber, "text:", rowElement.Text())
		
		// first column is row count
		// second column is time range
		if columnNumber == 2 {
			_timerange, err := parseTimeRange(rowElement.Text())
			if err != nil {
				fmt.Println("error parsing time range", err)
				return
			}
			timeRange = _timerange
		// other columns are lessons
		} else if columnNumber > scheduleStartColumn {
			if utils.IsEmptyOrInvisible(rowElement.Text()) {
				return
			}
			lessons, err := parseLessons(rowElement, timeRange)
			if err != nil {
				fmt.Println("error parsing lessons", err)
				return
			}
			//dayOfWeek := columnNumber - scheduleStartColumn + 1
			fmt.Println("lessons:", lessons)
			//schedule[dayOfWeek] = append(schedule[dayOfWeek], lessons)
		}
	})

	return schedule, nil
}

func ScrapeDivision(index uint) (*Division, error) {
	url := Config.OptivumBaseUrl + fmt.Sprintf(Config.DivisionEndpoint, index)
	fmt.Printf("scraping division from URL: %s\n", url)

	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching URL: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, fmt.Errorf("error loading HTML: %w", err)
	}

	division := Division{
		Index:      index,
		Designator: "",
		FullName:   "",
		Schedule:   Schedule{},
	}

	designator, fullName, err := scrapeTitle(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division title: %w", err)
	}
	division.Designator = designator
	division.FullName = fullName
	fmt.Printf("designator: %s\nfull name: %s\n", division.Designator, division.FullName)

	schedule, err := scrapeSchedule(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	division.Schedule = schedule

	return &division, nil
}

func Initialize() error {
	Config = config.Global.Scraper
	GeneralConfig = config.Global.General
	return nil
}
