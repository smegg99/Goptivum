package scraper

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"smuggr.xyz/optivum-bsf/common/models"
	"smuggr.xyz/optivum-bsf/common/utils"
	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/observer"

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

func waitForFirstRefresh() {
	var wg sync.WaitGroup

	divisionObservers := len(DivisionsScraperResource.Hub.GetAllObservers(true))
	teacherObservers := len(TeachersScraperResource.Hub.GetAllObservers(true))
	roomObservers := len(RoomsScraperResource.Hub.GetAllObservers(true))

	totalObservers := divisionObservers + teacherObservers + roomObservers

	if totalObservers > 0 {
        wg.Add(totalObservers)
    } else {
        fmt.Println("no observers to wait for")
        return
    }

	waitForRefresh := func(ch <-chan int64, count int) {
		fmt.Println("waiting for refresh:", count)
		for i := 0; i < count; i++ {
			go func() {
				<-ch
				select {
				case <-ch:
					wg.Done()
				case <-time.After(20 * time.Second):
					fmt.Println("timed out waiting for refresh")
					wg.Done()
				}
			}()
		}
	}

	waitForRefresh(DivisionsScraperResource.RefreshChan, divisionObservers)
	waitForRefresh(TeachersScraperResource.RefreshChan, teacherObservers)
	waitForRefresh(RoomsScraperResource.RefreshChan, roomObservers)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Some observers might not refresh so we
	// need to wait for a certain amount of time
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
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
			//fmt.Println("dayOfWeek:", dayOfWeek, "lessons:", lessons, workDays)

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

func newDivisionObserver(index int64, refreshChan *chan int64) *observer.Observer {
	extractFunc := func(doc *goquery.Document) string {
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

	callbackFunc := func() {
		division, err := ScrapeDivision(index)
		if err != nil {
			fmt.Printf("error scraping division: %v\n", err)
			return
		}

		*refreshChan <- index

		if err := datastore.SetDivision(division); err != nil {
			fmt.Printf("error saving division: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Division, index)
	interval := time.Duration((index + 1) / 10 + 5) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newTeacherObserver(index int64, refreshChan *chan int64) *observer.Observer {
	extractFunc := func(doc *goquery.Document) string {
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

	callbackFunc := func() {
		teacher, err := ScrapeTeacher(index)
		if err != nil {
			fmt.Printf("error scraping teacher: %v\n", err)
			return
		}

		*refreshChan <- index

		if err := datastore.SetTeacher(teacher); err != nil {
			fmt.Printf("error saving teacher: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Teacher, index)
	interval := time.Duration((index + 1) / 10 + 5) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newRoomObserver(index int64, refreshChan *chan int64) *observer.Observer {
	extractFunc := func(doc *goquery.Document) string {
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

	callbackFunc := func() {
		room, err := ScrapeRoom(index)
		if err != nil {
			fmt.Printf("error scraping room: %v\n", err)
			return
		}

		*refreshChan <- index

		if err := datastore.SetRoom(room); err != nil {
			fmt.Printf("error saving room: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Room, index)
	interval := time.Duration((index + 1) / 10 + 5) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}