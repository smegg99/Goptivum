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

var Config config.ScraperConfig

func splitDivisionTitle(s string) (string, string) {
	parts := strings.Split(s, " ")
	if len(parts) < 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func splitTeacherTitle(s string) (string, string) {
	parts := strings.Split(s, " ")
	if len(parts) < 2 {
		return "", ""
	}

	rawDesignator := strings.TrimSpace(parts[1])
	rawDesignator = strings.Trim(rawDesignator, "()")

	return strings.TrimSpace(parts[0]), rawDesignator
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

// TODO: Refactor the divisionDesignator scraping to a separate function
func parseLesson(rowElement *goquery.Selection, timeRange TimeRange, divisionDesignator string) (Lesson, error) {
	lessonName := rowElement.Find("span.p").First().Text()
	// Some lessons contain only the table data with embedded text only
	if lessonName == "" {
		lessonName = rowElement.Text()
	}

	teacher := rowElement.Find("a.n").First().Text()
	room := rowElement.Find("a.s").First().Text()
	lesson := Lesson{
		TimeRange: timeRange,
		FullName:  lessonName,
		Teacher:   teacher,
		Division:  divisionDesignator,
		Room:      room,
	}
	return lesson, nil
}

func parseLessons(rowElement *goquery.Selection, timeRange TimeRange, divisionDesignator string) ([]Lesson, error) {
	lessons := []Lesson{}
	innerSpanElements := rowElement.Find("span > span.p")
	if innerSpanElements.Length() > 1 {
		innerSpanElements.Each(func(i int, s *goquery.Selection) {
			parentSelection := s.Parent()
			lesson, err := parseLesson(parentSelection, timeRange, divisionDesignator)
			if err != nil {
				fmt.Println("error parsing lesson", err)
				return
			}
			lessons = append(lessons, lesson)
		})
	} else {
		lesson, err := parseLesson(rowElement, timeRange, divisionDesignator)
		if err != nil {
			fmt.Println("error parsing lesson", err)
			return nil, err
		}
		lessons = append(lessons, lesson)
	}

	return lessons, nil
}

func scrapeDivisionTitle(doc *goquery.Document) (string, string, error) {
	titleSelection := doc.Find("body > table > tbody > tr > td").First()
	if titleSelection.Length() == 0 {
		return "", "", fmt.Errorf("no division title found")
	}

	title := titleSelection.Text()
	designator, fullName := splitDivisionTitle(title)

	return designator, fullName, nil
}

func scrapeTeacherTitle(doc *goquery.Document) (string, string, error) {
	titleSelection := doc.Find("body > table > tbody > tr > td").First()
	if titleSelection.Length() == 0 {
		return "", "", fmt.Errorf("no division title found")
	}

	title := titleSelection.Text()
	designator, fullName := splitTeacherTitle(title)

	return designator, fullName, nil
}

func scrapeRoomTitle(doc *goquery.Document) (string, error) {
	titleSelection := doc.Find("body > table > tbody > tr > td").First()
	if titleSelection.Length() == 0 {
		return "", fmt.Errorf("no room title found")
	}

	title := titleSelection.Text()
	return title, nil
}

func scrapeSchedule(doc *goquery.Document, divisionDesignator string) (Schedule, error) {
	var schedule Schedule
	var timeRange TimeRange

	rowsSelection := doc.Find("table.tabela > tbody > tr")
	firstRow := rowsSelection.First()
	if firstRow.Length() == 0 {
		return schedule, fmt.Errorf("no rows found")
	}

	columnNumber := 0
	dayOfWeek := 0
	columnsCount := firstRow.Children().Length()

	rowsLength := doc.Find("table.tabela > tbody > tr > td.nr").Length() + 1
	lessonsLength := doc.Find("table.tabela > tbody > tr > td.l").Length()
	
	// First row is the table headers row so it doesn't count
	scheduleStartColumn := columnsCount - (lessonsLength / (rowsLength - 1))
	workDays := columnsCount - scheduleStartColumn

	schedule = make(Schedule, workDays)
	for i := range schedule {
		schedule[i] = [][]Lesson{}
	}

	doc.Find("table.tabela > tbody > tr > td").Each(func(i int, rowElement *goquery.Selection) {
		if columnNumber >= columnsCount {
			fmt.Println()
			columnNumber = 1
		} else {
			columnNumber++
		}

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
			if dayOfWeek < workDays {
				dayOfWeek++
			} else {
				dayOfWeek = 1
			}

			if utils.IsEmptyOrInvisible(rowElement.Text()) {
				return
			}
			lessons, err := parseLessons(rowElement, timeRange, divisionDesignator)
			if err != nil {
				fmt.Println("error parsing lessons", err)
				return
			}
			fmt.Println("dayOfWeek:", dayOfWeek, "lessons:", lessons, workDays)

			schedule[dayOfWeek-1] = append(schedule[dayOfWeek-1], lessons)
		}
	})

	return schedule, nil
}

func ScrapeDivision(index uint) (*Division, error) {
	endpoint := fmt.Sprintf(Config.Endpoints.Division, index)
	doc, err := OpenDoc(endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	division := Division{
		Index:      index,
		Designator: "",
		FullName:   "",
		Schedule:   Schedule{},
	}

	designator, fullName, err := scrapeDivisionTitle(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division title: %w", err)
	}
	division.Designator = designator
	division.FullName = fullName

	schedule, err := scrapeSchedule(doc, designator)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	division.Schedule = schedule

	return &division, nil
}

func ScrapeTeacher(index uint) (*Teacher, error) {
	endpoint := fmt.Sprintf(Config.Endpoints.Teacher, index)
	doc, err := OpenDoc(endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	teacher := Teacher{
		Designator: "",
		FullName:   "",
		Schedule:   Schedule{},
	}

	designator, fullName, err := scrapeTeacherTitle(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division title: %w", err)
	}
	teacher.Designator = designator
	teacher.FullName = fullName

	schedule, err := scrapeSchedule(doc, designator)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	teacher.Schedule = schedule

	return &teacher, nil
}

func ScrapeRoom(index uint) (*Room, error) {
	endpoint := fmt.Sprintf(Config.Endpoints.Room, index)
	doc, err := OpenDoc(endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	room := Room{
		Designator: "",
		Schedule:   Schedule{},
	}

	designator, err := scrapeRoomTitle(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division title: %w", err)
	}
	room.Designator = designator

	schedule, err := scrapeSchedule(doc, designator)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	room.Schedule = schedule

	return &room, nil
}

func OpenDoc(endpoint string) (*goquery.Document, error) {
	url := fmt.Sprintf("%s%s", Config.BaseUrl, endpoint)
	fmt.Printf("scraping teacher from URL: %s\n", url)

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

	return doc, nil
}

func Initialize() error {
	Config = config.Global.Scraper
	return nil
}
