// scraper/scraper.go
package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/common/models"
	"smuggr.xyz/optivum-bsf/common/utils"
	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/hub"
	"smuggr.xyz/optivum-bsf/core/observer"

	"github.com/PuerkitoBio/goquery"
)

var Config config.ScraperConfig

type ResourceType string

const (
	DivisionResource ResourceType = "division"
	TeacherResource  ResourceType = "teacher"
	RoomResource     ResourceType = "room"
)

func (t ResourceType) String() string {
	return string(t)
}

type ScraperResource struct {
	Indexes     []int64
	Designators *models.Designators
	Observer    *observer.Observer
	Hub         *hub.Hub
	IndexRegex  *regexp.Regexp
	Mu          *sync.RWMutex
	RefreshChan chan int64
	Type        ResourceType
}

func NewScraperResource(indexRegex *regexp.Regexp, resourceType ResourceType) *ScraperResource {
	return &ScraperResource{
		Indexes:     []int64{},
		Designators: &models.Designators{Designators: make(map[string]int64)},
		Observer:    &observer.Observer{},
		Hub:         &hub.Hub{},
		IndexRegex:  indexRegex,
		Mu:          &sync.RWMutex{},
		RefreshChan: make(chan int64),
		Type:        resourceType,
	}
}

func (s *ScraperResource) StartHub() {
	if s.Hub != nil {
		s.Hub.Start()
	}
}

func (s *ScraperResource) StopHub() {
	if s.Hub != nil {
		s.Hub.Stop()
	}
}

func (s *ScraperResource) UpdateDesignator(newDesignator string, index int64) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	for _designator, _index := range s.Designators.Designators {
		if index == _index {
			delete(s.Designators.Designators, _designator)
		}
	}
	s.Designators.Designators[newDesignator] = index
}

func (s *ScraperResource) RemoveDesignator(index int64) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	for designator, _index := range s.Designators.Designators {
		if index == _index {
			delete(s.Designators.Designators, designator)
		}
	}

	s.Mu.Lock()
	for key, _index := range s.Designators.Designators {
		if _index == index {
			delete(s.Designators.Designators, key)
			if len(s.Designators.Designators) == 0 {
				break
			}
		}
	}
	s.Mu.Unlock()
}

func (s *ScraperResource) UpdateIndexes(indexes []int64) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Indexes = indexes
}

func (s *ScraperResource) AddObserver(o *observer.Observer) {
	s.Hub.AddObserver(o)
}

func (s *ScraperResource) newObserver(index int64) *observer.Observer {
	switch s.Type {
	case DivisionResource:
		return newDivisionObserver(index, &s.RefreshChan)
	case TeacherResource:
		return newTeacherObserver(index, &s.RefreshChan)
	case RoomResource:
		return newRoomObserver(index, &s.RefreshChan)
	}

	return nil
}

func (s *ScraperResource) removeFromDatastore(index int64) error {
	switch s.Type {
	case DivisionResource:
		return datastore.DeleteDivision(index)
	case TeacherResource:
		return datastore.DeleteTeacher(index)
	case RoomResource:
		return datastore.DeleteRoom(index)
	}

	return nil
}

func (s *ScraperResource) RefreshObservers() {
	existingIndexes := make(map[int64]bool)
	for _, index := range s.Indexes {
		existingIndexes[index] = true
		if s.Hub.GetObserver(index) == nil {
			fmt.Printf("adding observer for resource (%s) %d\n", s.Type, index)
			observer := s.newObserver(index)
			s.Hub.AddObserver(observer)
		}
	}

	observersCopy := s.Hub.GetAllObservers()
	if len(observersCopy) > 0 {
		delete(observersCopy, 0) // Remove the list observer
	}
	for index := range observersCopy {
		if !existingIndexes[index] {
			fmt.Printf("deleting observer for resource (%s) %d\n", s.Type, index)
			s.Hub.RemoveObserver(index)
			if err := s.removeFromDatastore(index); err != nil {
				fmt.Printf("error deleting resource (%s) from datastore: %v\n", s.Type, err)
			}

			s.RemoveDesignator(index)
		}
	}

	fmt.Printf("observing resource (%s) with %d observer(s)...\n", s.Type, len(s.Hub.GetAllObservers()))
}

var (
	DivisionIndexRegex = regexp.MustCompile(`o(\d+)\.html`)
	TeacherIndexRegex  = regexp.MustCompile(`n(\d+)\.html`)
	RoomIndexRegex     = regexp.MustCompile(`s(\d+)\.html`)
)

var (
	DivisionsScraperResource *ScraperResource
	TeachersScraperResource  *ScraperResource
	RoomsScraperResource     *ScraperResource
)

func ScrapeDivision(index int64) (*models.Division, error) {
	endpoint := makeDivisionEndpoint(index)
	doc, err := utils.OpenDoc(Config.BaseUrl, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	division := models.Division{
		Index:      index,
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

	DivisionsScraperResource.UpdateDesignator(designator, index)

	schedule, err := scrapeSchedule(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	division.Schedule = schedule

	return &division, nil
}

func ScrapeTeacher(index int64) (*models.Teacher, error) {
	endpoint := makeTeacherEndpoint(index)
	doc, err := utils.OpenDoc(Config.BaseUrl, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	teacher := models.Teacher{
		Index:      index,
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

	TeachersScraperResource.UpdateDesignator(designator, index)

	schedule, err := scrapeSchedule(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	teacher.Schedule = schedule

	return &teacher, nil
}

func ScrapeRoom(index int64) (*models.Room, error) {
	endpoint := makeRoomEndpoint(index)
	doc, err := utils.OpenDoc(Config.BaseUrl, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	room := models.Room{
		Index:      index,
		Designator: "",
		Schedule:   &models.Schedule{},
	}

	designator, err := scrapeRoomTitle(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division title: %w", err)
	}
	room.Designator = designator

	RoomsScraperResource.UpdateDesignator(designator, index)

	schedule, err := scrapeSchedule(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division schedule: %w", err)
	}
	room.Schedule = schedule

	return &room, nil
}

func ScrapeDivisionsIndexes() ([]int64, error) {
	doc, err := utils.OpenDoc(Config.BaseUrl, Config.Endpoints.DivisionsList)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	indexes := []int64{}
	doc.Find("a").Each(func(index int, element *goquery.Selection) {
		href, exists := element.Attr("href")
		if exists {
			match := DivisionIndexRegex.FindStringSubmatch(href)
			if len(match) > 1 {
				number := match[1]
				num, err := strconv.ParseInt(number, 10, 64)
				if err != nil {
					fmt.Printf("error converting number to uint: %v\n", err)
					return
				}
				endpoint := makeDivisionEndpoint(num)
				_, err = utils.OpenDoc(Config.BaseUrl, endpoint)
				if err != nil {
					fmt.Printf("error opening division document: %v\n", err)
					return
				}
				indexes = append(indexes, num)
			}
		}
	})

	return indexes, nil
}

func ScrapeTeachersIndexes() ([]int64, error) {
	doc, err := utils.OpenDoc(Config.BaseUrl, Config.Endpoints.TeachersList)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	indexes := []int64{}

	doc.Find("a").Each(func(index int, element *goquery.Selection) {
		href, exists := element.Attr("href")
		if exists {
			match := TeacherIndexRegex.FindStringSubmatch(href)
			if len(match) > 1 {
				number := match[1]
				num, err := strconv.ParseInt(number, 10, 64)
				if err != nil {
					fmt.Printf("error converting number to uint: %v\n", err)
					return
				}
				endpoint := makeTeacherEndpoint(num)
				_, err = utils.OpenDoc(Config.BaseUrl, endpoint)
				if err != nil {
					fmt.Printf("error opening teacher document: %v\n", err)
					return
				}
				indexes = append(indexes, num)
			}
		}
	})

	return indexes, nil
}

func ScrapeRoomsIndexes() ([]int64, error) {
	doc, err := utils.OpenDoc(Config.BaseUrl, Config.Endpoints.RoomsList)
	if err != nil {
		return nil, fmt.Errorf("error opening document: %w", err)
	}

	indexes := []int64{}

	doc.Find("a").Each(func(index int, element *goquery.Selection) {
		href, exists := element.Attr("href")
		if exists {
			match := RoomIndexRegex.FindStringSubmatch(href)
			if len(match) > 1 {
				number := match[1]
				num, err := strconv.ParseInt(number, 10, 64)
				if err != nil {
					fmt.Printf("error converting number to uint: %v\n", err)
					return
				}
				endpoint := makeRoomEndpoint(num)
				_, err = utils.OpenDoc(Config.BaseUrl, endpoint)
				if err != nil {
					fmt.Printf("error opening room document: %v\n", err)
					return
				}
				indexes = append(indexes, num)
			}
		}
	})

	return indexes, nil
}

func Initialize() error {
	fmt.Println("initializing scraper")
	Config = config.Global.Scraper

	DivisionsScraperResource = NewScraperResource(DivisionIndexRegex, DivisionResource)
	TeachersScraperResource  = NewScraperResource(TeacherIndexRegex, TeacherResource)
	RoomsScraperResource     = NewScraperResource(RoomIndexRegex, RoomResource)

	divisionsIndexes, err := ScrapeDivisionsIndexes()
	if err != nil {
		return fmt.Errorf("error scraping divisions indexes: %w", err)
	}
	DivisionsScraperResource.Indexes = divisionsIndexes

	teachersIndexes, err := ScrapeTeachersIndexes()
	if err != nil {
		return fmt.Errorf("error scraping teachers indexes: %w", err)
	}
	TeachersScraperResource.Indexes = teachersIndexes

	roomsIndexes, err := ScrapeRoomsIndexes()
	if err != nil {
		return fmt.Errorf("error scraping rooms indexes: %w", err)
	}
	RoomsScraperResource.Indexes = roomsIndexes

	divisionsIndexesLength := int64(len(divisionsIndexes))
	teachersIndexesLength := int64(len(teachersIndexes))
	roomsIndexesLength := int64(len(roomsIndexes))

	fmt.Printf("starting with %d divisions, %d teachers, %d rooms\n", divisionsIndexesLength, teachersIndexesLength, roomsIndexesLength)
	
	if divisionsIndexesLength == 0 {
		fmt.Printf("no divisions found despite %d workers\n", Config.Quantities.Workers.Division)
	}
	if teachersIndexesLength == 0 {
		fmt.Printf("no teachers found despite %d workers\n", Config.Quantities.Workers.Teacher)
	}
	if roomsIndexesLength == 0 {
		fmt.Printf("no rooms found despite %d workers\n", Config.Quantities.Workers.Room)
	}

	DivisionsScraperResource.Hub = hub.NewHub(Config.Quantities.Workers.Division)
	RoomsScraperResource.Hub = hub.NewHub(Config.Quantities.Workers.Teacher)
	TeachersScraperResource.Hub = hub.NewHub(Config.Quantities.Workers.Room)

	ObserveDivisions(&DivisionsScraperResource.RefreshChan)
	ObserveTeachers(&TeachersScraperResource.RefreshChan)
	ObserveRooms(&RoomsScraperResource.RefreshChan)

	return nil
}

func Cleanup() {
	fmt.Println("cleaning scraper")
	DivisionsScraperResource.StopHub()
	TeachersScraperResource.StopHub()
	RoomsScraperResource.StopHub()
}
