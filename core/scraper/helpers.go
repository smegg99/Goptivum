package scraper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"smuggr.xyz/goptivum/common/models"
	"smuggr.xyz/goptivum/common/utils"
	"smuggr.xyz/goptivum/core/datastore"
	"smuggr.xyz/goptivum/core/observer"

	"github.com/PuerkitoBio/goquery"
)

func makeDivisionEndpoint(index int64) string {
	return fmt.Sprintf(Config.Endpoints.Division, index)
}

func makeTeacherEndpoint(index int64) string {
	return fmt.Sprintf(Config.Endpoints.Teacher, index)
}

func makeRoomEndpoint(index int64) string {
	return fmt.Sprintf(Config.Endpoints.Room, index)
}

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

func parseTimeRange(s string) (models.TimeRange, error) {
	s = strings.ReplaceAll(s, " ", "")
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return models.TimeRange{}, fmt.Errorf("invalid time range: %s", s)
	}

	_start := strings.TrimSpace(parts[0])
	_end := strings.TrimSpace(parts[1])

	startParts := strings.Split(_start, ":")
	if len(startParts) != 2 {
		return models.TimeRange{}, fmt.Errorf("invalid start time: %s", _start)
	}
	startHour, err := strconv.ParseInt(startParts[0], 10, 32)
	if err != nil {
		return models.TimeRange{}, fmt.Errorf("invalid start hour: %s %v", startParts[0], err)
	}
	startMinute, err := strconv.ParseInt(startParts[1], 10, 32)
	if err != nil {
		return models.TimeRange{}, fmt.Errorf("invalid start minute: %s %v", startParts[1], err)
	}

	endParts := strings.Split(_end, ":")
	if len(endParts) != 2 {
		return models.TimeRange{}, fmt.Errorf("invalid end time: %s %v", _end, err)
	}
	endHour, err := strconv.ParseInt(endParts[0], 10, 32)
	if err != nil {
		return models.TimeRange{}, fmt.Errorf("invalid end hour: %s %v", endParts[0], err)
	}

	endMinute, err := strconv.ParseInt(endParts[1], 10, 32)
	if err != nil {
		return models.TimeRange{}, fmt.Errorf("invalid end minute: %s %v", endParts[1], err)
	}

	start := models.Timestamp{
		Hour:   startHour,
		Minute: startMinute,
	}

	end := models.Timestamp{
		Hour:   endHour,
		Minute: endMinute,
	}

	return models.TimeRange{Start: &start, End: &end}, nil
}

func parseLesson(rowElement *goquery.Selection, timeRange *models.TimeRange) ([]*models.Lesson, error) {
	var lessons []*models.Lesson

	html, err := rowElement.Html()
	if err != nil {
		return nil, fmt.Errorf("error getting HTML content: %v", err)
	}

	html = strings.ReplaceAll(html, "<br>", "<br/>")
	segments := strings.Split(html, "<br/>")

	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "&nbsp;" {
			continue
		}

		segmentHTML := "<div>" + segment + "</div>"

		segmentDoc, err := goquery.NewDocumentFromReader(strings.NewReader(segmentHTML))
		if err != nil {
			fmt.Println("error parsing segment:", err)
			continue
		}

		lessonName := strings.TrimSpace(segmentDoc.Find("span.p").First().Text())
		if lessonName == "" {
			lessonName = strings.TrimSpace(segmentDoc.Text())
		}
		teacher := strings.TrimSpace(segmentDoc.Find("a.n").First().Text())
		division := strings.TrimSpace(segmentDoc.Find("a.o").First().Text())
		room := strings.TrimSpace(segmentDoc.Find("a.s").First().Text())

		lesson := &models.Lesson{
			TimeRange:          timeRange,
			FullName:           lessonName,
			TeacherDesignator:  teacher,
			DivisionDesignator: division,
			RoomDesignator:     room,
		}
		lessons = append(lessons, lesson)
	}

	return lessons, nil
}
func scrapeTitle(doc *goquery.Document) (string, error) {
	titleSelection := doc.Find("span.tytulnapis").First()
	if titleSelection.Length() == 0 {
		return "", fmt.Errorf("no title found")
	}

	return strings.TrimSpace(titleSelection.Text()), nil
}

func scrapeDivisionTitle(doc *goquery.Document) (string, string, error) {
	title, err := scrapeTitle(doc)
	if err != nil {
		return "", "", fmt.Errorf("error scraping title: %w", err)
	}

	designator, fullName := splitDivisionTitle(title)

	return designator, fullName, nil
}

func scrapeTeacherTitle(doc *goquery.Document) (string, string, error) {
	title, err := scrapeTitle(doc)
	if err != nil {
		return "", "", fmt.Errorf("error scraping title: %w", err)
	}

	fullName, designator := splitTeacherTitle(title)

	return designator, fullName, nil
}

func splitRoomTitle(s string) (string, string) {
	parts := strings.SplitN(s, " ", 2)
	if len(parts) == 0 {
		return "", ""
	}
	designator := strings.TrimSpace(parts[0])
	var fullName string
	if len(parts) > 1 {
		fullName = strings.TrimSpace(parts[1])
	} else {
		fullName = designator
	}
	return designator, fullName
}

func scrapeRoomTitle(doc *goquery.Document) (string, string, error) {
	title, err := scrapeTitle(doc)
	if err != nil {
		return "", "", fmt.Errorf("error scraping title: %w", err)
	}
	designator, fullName := splitRoomTitle(title)

	return designator, fullName, nil
}

func scrapeSchedule(doc *goquery.Document) (*models.Schedule, error) {
	var schedule *models.Schedule
	var timeRange *models.TimeRange

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

	schedule = &models.Schedule{
		ScheduleDays: make([]*models.ScheduleDay, workDays),
	}

	for i := 0; i < workDays; i++ {
		schedule.ScheduleDays[i] = &models.ScheduleDay{
			LessonGroups: []*models.LessonGroup{},
		}
	}

	doc.Find("table.tabela > tbody > tr > td").Each(func(i int, rowElement *goquery.Selection) {
		if columnNumber >= columnsCount {
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
			timeRange = &_timerange
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
			lessons, err := parseLesson(rowElement, timeRange)
			if err != nil {
				fmt.Println("error parsing lessons", err)
				return
			}
			
			lessonGroup := &models.LessonGroup{
				Lessons: lessons,
			}

			schedule.ScheduleDays[dayOfWeek-1].LessonGroups = append(
				schedule.ScheduleDays[dayOfWeek-1].LessonGroups,
				lessonGroup,
			)
		}
	})

	return schedule, nil
}

func parseDutyTimestamp(s string) (models.TimeRange, error) {
    s = strings.TrimSpace(s)

    // Remove the leading index and dot (e.g., "0. ", "1. ")
    if idx := strings.Index(s, " "); idx != -1 {
        s = s[idx+1:]
    }

    parts := strings.Split(s, "-")
    if len(parts) != 2 {
        return models.TimeRange{}, fmt.Errorf("invalid time range: %s", s)
    }

    _start := strings.TrimSpace(parts[0])
    _end := strings.TrimSpace(parts[1])

    startParts := strings.Split(_start, ":")
    if len(startParts) != 2 {
        return models.TimeRange{}, fmt.Errorf("invalid start time: %s", _start)
    }
    startHour, err := strconv.ParseInt(startParts[0], 10, 32)
    if err != nil {
        return models.TimeRange{}, fmt.Errorf("invalid start hour: %s %v", startParts[0], err)
    }
    startMinute, err := strconv.ParseInt(startParts[1], 10, 32)
    if err != nil {
        return models.TimeRange{}, fmt.Errorf("invalid start minute: %s %v", startParts[1], err)
    }

    endParts := strings.Split(_end, ":")
    if len(endParts) != 2 {
        return models.TimeRange{}, fmt.Errorf("invalid end time: %s", _end)
    }
    endHour, err := strconv.ParseInt(endParts[0], 10, 32)
    if err != nil {
        return models.TimeRange{}, fmt.Errorf("invalid end hour: %s %v", endParts[0], err)
    }
    endMinute, err := strconv.ParseInt(endParts[1], 10, 32)
    if err != nil {
        return models.TimeRange{}, fmt.Errorf("invalid end minute: %s %v", endParts[1], err)
    }

    start := models.Timestamp{
        Hour:   startHour,
        Minute: startMinute,
    }

    end := models.Timestamp{
        Hour:   endHour,
        Minute: endMinute,
    }

    return models.TimeRange{Start: &start, End: &end}, nil
}

func scrapeTeachersOnDuty(doc *goquery.Document) ([]*models.TeachersOnDuty, error) {
	var teachersOnDutyList []*models.TeachersOnDuty

	doc.Find("body h2").Each(func(i int, dayHeader *goquery.Selection) {
		dayName := strings.TrimSpace(dayHeader.Text())
		dayTable := dayHeader.NextFiltered("table")

		if dayTable.Length() == 0 {
			fmt.Printf("No table found for day: %s\n", dayName)
			return
		}

		var dutyGroups []*models.DutyGroup

		dayTable.Find("tbody > tr").Each(func(j int, row *goquery.Selection) {
			if j == 0 {
				return
			}

			columns := row.Find("th, td")
			if columns.Length() < 2 {
				return
			}

			timeRangeText := strings.TrimSpace(columns.First().Text())
			timeRange, err := parseDutyTimestamp(timeRangeText)
			if err != nil {
				fmt.Printf("Error parsing time range '%s': %v\n", timeRangeText, err)
				return
			}

			var duties []*models.Duty
			columns.NextAll().Each(func(k int, dutyCell *goquery.Selection) {
				placeFullName := dayTable.Find("tr").First().Find("th").Eq(k + 1).Text()
				teacherFullName := strings.TrimSpace(dutyCell.Text())

				if teacherFullName != "" {
					duties = append(duties, &models.Duty{
						TeacherFullName: teacherFullName,
						PlaceFullName:   placeFullName,
					})
				}
			})

			dutyGroups = append(dutyGroups, &models.DutyGroup{
				TimeRange: &timeRange,
				Duties:    duties,
			})
		})

		// Add to the result list
		teachersOnDutyList = append(teachersOnDutyList, &models.TeachersOnDuty{
			DayOfWeek:  dayName,
			DutyGroups: dutyGroups,
		})
	})

	return teachersOnDutyList, nil
}

func parseDateRange(s string) (models.TimeRange, error) {
    parts := strings.Split(s, " - ")
    if len(parts) != 2 {
        return models.TimeRange{}, fmt.Errorf("invalid date range: %s", s)
    }

    startStr := strings.TrimSpace(parts[0])
    endStr := strings.TrimSpace(parts[1])

    startTimestamp, err := parseDateToTimestamp(startStr)
    if err != nil {
        return models.TimeRange{}, fmt.Errorf("error parsing start date '%s': %v", startStr, err)
    }

    endTimestamp, err := parseDateToTimestamp(endStr)
    if err != nil {
        return models.TimeRange{}, fmt.Errorf("error parsing end date '%s': %v", endStr, err)
    }

    return models.TimeRange{
        Start: &startTimestamp,
        End:   &endTimestamp,
    }, nil
}

func parseDateToTimestamp(dateStr string) (models.Timestamp, error) {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return models.Timestamp{}, fmt.Errorf("invalid date format: %s", dateStr)
	}

	return models.Timestamp{
		Year:   int64(t.Year()),
		Month:  int64(t.Month()),
		Day:    int64(t.Day()),
		Hour:   0,
		Minute: 0,
		Second: 0,
	}, nil
}

func parseDivisionDesignators(text string) []string {
	designators := strings.Split(text, ",")
	for i, d := range designators {
		designators[i] = strings.TrimSpace(d)
	}
	return designators
}

func scrapePractices(doc *goquery.Document) (*models.Practices, error) {
	var practices models.Practices

	tableSelection := doc.Find("body h2 table")
	if tableSelection.Length() == 0 {
		return nil, fmt.Errorf("no table found in the document")
	}

	tableSelection.Find("tbody > tr").Each(func(i int, row *goquery.Selection) {
		if i == 0 {
			return
		}

		columns := row.Find("th, td")
		if columns.Length() < 2 {
			fmt.Println("not enough columns in row")
			return
		}

		divisionText := strings.TrimSpace(columns.Eq(0).Text())
		divisionDesignators := parseDivisionDesignators(divisionText)

		timeRangeText := strings.TrimSpace(columns.Eq(1).Text())
		timeRange, err := parseDateRange(timeRangeText)
		if err != nil {
			fmt.Printf("error parsing time range '%s': %v\n", timeRangeText, err)
			return
		}

		practicesGroup := &models.PracticesGroup{
			DivisionDesignators: divisionDesignators,
			TimeRange:           &timeRange,
		}

		practices.PracticesGroups = append(practices.PracticesGroups, practicesGroup)
	})

	return &practices, nil
}

func newDivisionObserver(index int64, refreshChan *chan int64) *observer.Observer {
	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var content []string
		doc.Find("table.tabela").Each(func(i int, table *goquery.Selection) {
			table.Find("td, th").Each(func(i int, s *goquery.Selection) {
				content = append(content, s.Text())
			})
			table.Find("a").Each(func(i int, s *goquery.Selection) {
				href, exists := s.Attr("href")
				if exists {
					content = append(content, href)
				}
			})
		})

		doc.Find("span.tytulnapis").Each(func(i int, s *goquery.Selection) {
			content = append(content, s.Text())
		})

		return strings.Join(content, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		division, err := ScrapeDivision(index)
		if err != nil {
			fmt.Printf("error scraping division: %v\n", err)
			return
		}

		if err := datastore.SetDivision(division); err != nil {
			fmt.Printf("error saving division: %v\n", err)
			return
		}

		if !o.FirstRun {
			*refreshChan <- index
		}
	}
	
	url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Division, index)
	interval := time.Duration((index+1)/10+5) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newTeacherObserver(index int64, refreshChan *chan int64) *observer.Observer {
	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var content []string
		doc.Find("table").Each(func(i int, table *goquery.Selection) {
			table.Find("td, th").Each(func(i int, s *goquery.Selection) {
				content = append(content, s.Text())
			})
		})

		doc.Find("span.tytulnapis").Each(func(i int, s *goquery.Selection) {
			content = append(content, s.Text())
		})

		return strings.Join(content, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		teacher, err := ScrapeTeacher(index)
		if err != nil {
			fmt.Printf("error scraping teacher: %v\n", err)
			return
		}

		if err := datastore.SetTeacher(teacher); err != nil {
			fmt.Printf("error saving teacher: %v\n", err)
			return
		}

		if !o.FirstRun {
			*refreshChan <- index
		}
	}

	url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Teacher, index)
	interval := time.Duration((index+1)/10+5) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newRoomObserver(index int64, refreshChan *chan int64) *observer.Observer {
	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var content []string

		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				content = append(content, href)
			}
		})

		doc.Find("span.tytulnapis").Each(func(i int, s *goquery.Selection) {
			content = append(content, s.Text())
		})

		return strings.Join(content, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		room, err := ScrapeRoom(index)
		if err != nil {
			fmt.Printf("error scraping room: %v\n", err)
			return
		}

		if err := datastore.SetRoom(room); err != nil {
			fmt.Printf("error saving room: %v\n", err)
			return
		}

		if !o.FirstRun {
			*refreshChan <- index
		}
	}

	url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Room, index)
	interval := time.Duration((index+1)/10+5) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newTeachersOnDutyObserver(refreshChan *chan int64) *observer.Observer {
	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var tds []string
		doc.Find("td").Each(func(i int, s *goquery.Selection) {
			tds = append(tds, s.Text())
		})
		return strings.Join(tds, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		teachersOnDutyWeek, err := ScrapeTeachersOnDuty()
		if err != nil {
			fmt.Printf("error scraping teachers on duty: %v\n", err)
			return
		}

		if err := datastore.SetTeachersOnDutyWeek(teachersOnDutyWeek); err != nil {
			fmt.Printf("error saving teachers on duty: %v\n", err)
			return
		}

		if !o.FirstRun {
			*refreshChan <- 0
		}
	}

	url := Config.BaseUrl + Config.Endpoints.TeachersOnDuty
	interval := 5 * time.Minute

	return observer.NewObserver(0, url, interval, extractFunc, callbackFunc)
}

func newPracticesObserver(refreshChan *chan int64) *observer.Observer {
	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var ths []string
		doc.Find("th").Each(func(i int, s *goquery.Selection) {
			ths = append(ths, s.Text())
		})
		return strings.Join(ths, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		practices, err := ScrapePractices()
		if err != nil {
			fmt.Printf("error scraping practices: %v\n", err)
			return
		}

		if err := datastore.SetPractices(practices); err != nil {
			fmt.Printf("error saving practices: %v\n", err)
			return
		}

		if !o.FirstRun {
			*refreshChan <- 0
		}
	}

	url := Config.BaseUrl + Config.Endpoints.Practices
	interval := 5 * time.Second

	return observer.NewObserver(0, url, interval, extractFunc, callbackFunc)
}