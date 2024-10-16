// scraper.go
package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/common/models"
	"smuggr.xyz/optivum-bsf/common/utils"
	"smuggr.xyz/optivum-bsf/core/observer"

	"github.com/PuerkitoBio/goquery"
)

var Config config.ScraperConfig

const (
	DivisionIndexRegex = `o(\d+)\.html`
	TeacherIndexRegex  = `n(\d+)\.html`
	RoomIndexRegex     = `s(\d+)\.html`
)

var (
	DivisionsIndexes []uint32
	TeachersIndexes  []uint32
	RoomsIndexes     []uint32

	DivisionsListObserver *observer.Observer
	DivisionsObservers    map[uint32]*observer.Observer
)

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

	start := models.Timestamp{
		Hour: 0,
		Minute: 0,
	}

	end := models.Timestamp{
		Hour: 0,
		Minute: 0,
	}

	return models.TimeRange{Start: &start, End: &end}, nil
}

// TODO: Refactor the divisionDesignator scraping to a separate function
func parseLesson(rowElement *goquery.Selection, timeRange *models.TimeRange, divisionDesignator string) (*models.Lesson, error) {
	lessonName := rowElement.Find("span.p").First().Text()
	// Some lessons contain only the table data with embedded text only
	if lessonName == "" {
		lessonName = rowElement.Text()
	}

	teacher := rowElement.Find("a.n").First().Text()
	room := rowElement.Find("a.s").First().Text()
	lesson := models.Lesson{
		TimeRange:           timeRange,
		FullName:            lessonName,
		TeacherDesignator:   teacher,
		DivisionDesignator:  divisionDesignator,
		RoomDesignator:      room,
	}
	return &lesson, nil
}

func parseLessons(rowElement *goquery.Selection, timeRange *models.TimeRange, divisionDesignator string) ([]*models.Lesson, error) {
	lessons := []*models.Lesson{}
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

func scrapeSchedule(doc *goquery.Document, divisionDesignator string) (*models.Schedule, error) {
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
			//fmt.Println()
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
			lessons, err := parseLessons(rowElement, timeRange, divisionDesignator)
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

func ScrapeDivision(index uint32) (*models.Division, error) {
	endpoint := fmt.Sprintf(Config.Endpoints.Division, index)
	doc, err := utils.OpenDoc(Config.BaseUrl, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	division := models.Division{
		Index:   	index,
		Designator: "",
		FullName:   "",
		Schedule:   &models.Schedule{},
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

func ScrapeTeacher(index uint32) (*models.Teacher, error) {
	endpoint := fmt.Sprintf(Config.Endpoints.Teacher, index)
	doc, err := utils.OpenDoc(Config.BaseUrl, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	teacher := models.Teacher{
		Index:  	index,
		Designator: "",
		FullName:   "",
		Schedule:   &models.Schedule{},
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

func ScrapeRoom(index uint32) (*models.Room, error) {
	endpoint := fmt.Sprintf(Config.Endpoints.Room, index)
	doc, err := utils.OpenDoc(Config.BaseUrl, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	room := models.Room{
		Index:   	index,
		Designator: "",
		Schedule:   &models.Schedule{},
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

func ScrapeDivisionsIndexes() ([]uint32, error) {
	doc, err := utils.OpenDoc(Config.BaseUrl, Config.Endpoints.DivisionsList)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	indexes := []uint32{}
	re := regexp.MustCompile(DivisionIndexRegex)

	doc.Find("a").Each(func(index int, element *goquery.Selection) {
		href, exists := element.Attr("href")
		if exists {
			match := re.FindStringSubmatch(href)
			if len(match) > 1 {
				number := match[1]
				num, err := strconv.ParseUint(number, 10, 32)
				if err != nil {
					fmt.Printf("error converting number to uint: %v\n", err)
					return
				}
				indexes = append(indexes, uint32(num))
			}
		}
	})

	return indexes, nil
}

func ScrapeTeachersIndexes() ([]uint32, error) {
	doc, err := utils.OpenDoc(Config.BaseUrl, Config.Endpoints.TeachersList)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	indexes := []uint32{}
	re := regexp.MustCompile(TeacherIndexRegex)

	doc.Find("a").Each(func(index int, element *goquery.Selection) {
		href, exists := element.Attr("href")
		if exists {
			match := re.FindStringSubmatch(href)
			if len(match) > 1 {
				number := match[1]
				num, err := strconv.ParseUint(number, 10, 32)
				if err != nil {
					fmt.Printf("error converting number to uint: %v\n", err)
					return
				}
				indexes = append(indexes, uint32(num))
			}
		}
	})

	return indexes, nil
}

func ScrapeRoomsIndexes() ([]uint32, error) {
	doc, err := utils.OpenDoc(Config.BaseUrl, Config.Endpoints.RoomsList)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	indexes := []uint32{}
	re := regexp.MustCompile(RoomIndexRegex)

	doc.Find("a").Each(func(index int, element *goquery.Selection) {
		href, exists := element.Attr("href")
		if exists {
			match := re.FindStringSubmatch(href)
			if len(match) > 1 {
				number := match[1]
				num, err := strconv.ParseUint(number, 10, 32)
				if err != nil {
					fmt.Printf("error converting number to uint: %v\n", err)
					return
				}
				indexes = append(indexes, uint32(num))
			}
		}
	})

	return indexes, nil
}

func ObserveDivisions() {
	fmt.Println("observing divisions")

	DivisionsObservers = make(map[uint32]*observer.Observer)

	newDivisionObserver := func(index uint32) *observer.Observer {
		url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Division, index)
		interval := time.Duration((index+1)/10 + 1) * time.Second 
		return observer.NewObserver(url, interval, func(doc *goquery.Document) string {
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

			return strings.Join(content, " ")
		})
	}

	startDivisionObserver := func(observer *observer.Observer, index uint32) {
		observer.Start(func() {
			fmt.Printf("refreshing division %d\n", index)
			_, err := ScrapeDivision(index)
			if err != nil {
				fmt.Printf("error scraping division: %v\n", err)
				return
			}
			//fmt.Println(division)
		})
	}

	refreshDivisionsObservers := func() {
		for _, index := range DivisionsIndexes {
			if _, exists := DivisionsObservers[index]; !exists {
				DivisionsObservers[index] = newDivisionObserver(index)
				startDivisionObserver(DivisionsObservers[index], index)
			}
		}

		fmt.Printf("observing divisions with %d observers...\n", len(DivisionsObservers))
	}

	DivisionsListObserver = observer.NewObserver(Config.BaseUrl+Config.Endpoints.DivisionsList, time.Second * 5, func(doc *goquery.Document) string {
		var hrefs []string
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	})

	DivisionsListObserver.Start(func() {
		fmt.Println("divisions list changed!")
		divisionsIndexes, err := ScrapeDivisionsIndexes()
		if err != nil {
			fmt.Printf("error scraping divisions indexes: %v\n", err)
			return
		}
		DivisionsIndexes = divisionsIndexes
		refreshDivisionsObservers()
	})

	refreshDivisionsObservers()
}

func Initialize() error {
	fmt.Println("initializing scraper")
	Config = config.Global.Scraper

	divisionsIndexes, err := ScrapeDivisionsIndexes()
	if err != nil {
		return fmt.Errorf("error scraping divisions indexes: %w", err)
	}
	DivisionsIndexes = divisionsIndexes

	teachersIndexes, err := ScrapeTeachersIndexes()
	if err != nil {
		return fmt.Errorf("error scraping teachers indexes: %w", err)
	}
	TeachersIndexes = teachersIndexes

	roomsIndexes, err := ScrapeRoomsIndexes()
	if err != nil {
		return fmt.Errorf("error scraping rooms indexes: %w", err)
	}
	RoomsIndexes = roomsIndexes

	fmt.Println("divisions indexes:", DivisionsIndexes)
	fmt.Println("teachers indexes:", TeachersIndexes)
	fmt.Println("rooms indexes:", RoomsIndexes)

	divisionsIndexesLength := uint32(len(DivisionsIndexes))
	teachersIndexesLength := uint32(len(TeachersIndexes))
	roomsIndexesLength := uint32(len(RoomsIndexes))

	if divisionsIndexesLength == 0 {
		return fmt.Errorf("no divisions found")
	} else if teachersIndexesLength == 0 {
		return fmt.Errorf("no teachers found")
	} else if roomsIndexesLength == 0 {
		return fmt.Errorf("no rooms found")
	}

	if divisionsIndexesLength != Config.Quantities.Divisions {
		fmt.Printf("expected %d divisions, found %d\n", Config.Quantities.Divisions, divisionsIndexesLength)
	}

	if teachersIndexesLength != Config.Quantities.Teachers {
		fmt.Printf("expected %d teachers, found %d\n", Config.Quantities.Teachers, teachersIndexesLength)
	}

	if roomsIndexesLength != Config.Quantities.Rooms {
		fmt.Printf("expected %d rooms, found %d\n", Config.Quantities.Rooms, roomsIndexesLength)
	}

	//ObserveDivisions()

	return nil
}
