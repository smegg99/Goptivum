// scraper/scraper.go
package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"smuggr.xyz/goptivum/common/config"
	"smuggr.xyz/goptivum/common/models"
	"smuggr.xyz/goptivum/common/utils"
	"smuggr.xyz/goptivum/core/datastore"
	"smuggr.xyz/goptivum/core/hub"
	"smuggr.xyz/goptivum/core/observer"

	"github.com/PuerkitoBio/goquery"
)

// TODO: Add the ability to add more resource indexes to the scraper, (SKat)

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

type MetadataType string

const (
	DesignatorMetadata MetadataType = "designator"
	FullNameMetadata   MetadataType = "fullname"
)

type ScraperResource struct {
	Indexes     []int64
	Metadata    *models.Metadata
	Observer    *observer.Observer
	Hub         *hub.Hub
	IndexRegex  *regexp.Regexp
	Mu          *sync.RWMutex
	RefreshChan chan int64
	Type        ResourceType
}

func NewScraperResource(indexRegex *regexp.Regexp, resourceType ResourceType) *ScraperResource {
	return &ScraperResource{
		Indexes: []int64{},
		Metadata: &models.Metadata{
			Designators: make(map[string]*models.Duplicates),
			FullNames:   make(map[string]*models.Duplicates),
		},
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

func (s *ScraperResource) IsIndexInMetadata(index int64, metadataType MetadataType) (bool, string) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	var metadata map[string]*models.Duplicates
	if metadataType == DesignatorMetadata {
		metadata = s.Metadata.Designators
	} else if metadataType == FullNameMetadata {
		metadata = s.Metadata.FullNames
	} else {
		return false, ""
	}

	for key, duplicates := range metadata {
		for _, _index := range duplicates.Values {
			if index == _index {
				return true, key
			}
		}
	}
	return false, ""
}

func (s *ScraperResource) GetDesignatorFromIndex(index int64) string {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	if inDuplicates, designator := s.IsIndexInMetadata(index, DesignatorMetadata); inDuplicates {
		return designator
	}

	return ""
}

func (s *ScraperResource) GetFullNameFromIndex(index int64) string {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	if inDuplicates, fullName := s.IsIndexInMetadata(index, FullNameMetadata); inDuplicates {
		return fullName
	}

	return ""
}

func (s *ScraperResource) GetIndexFromDesignator(designator string) *models.Duplicates {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return s.Metadata.Designators[designator]
}

func (s *ScraperResource) GetIndexFromFullName(fullName string) *models.Duplicates {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return s.Metadata.FullNames[fullName]
}

func (s *ScraperResource) UpdateMetadata(newDesignator, newFullName string, index int64) {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for designator, duplicates := range s.Metadata.Designators {
		for i, _index := range duplicates.Values {
			if index == _index {
				duplicates.Values = append(duplicates.Values[:i], duplicates.Values[i+1:]...)
				if len(duplicates.Values) == 0 {
					delete(s.Metadata.Designators, designator)
				}
				break
			}
		}
	}
	if _, exists := s.Metadata.Designators[newDesignator]; !exists {
		s.Metadata.Designators[newDesignator] = &models.Duplicates{}
	}
	s.Metadata.Designators[newDesignator].Values = append(s.Metadata.Designators[newDesignator].Values, index)

	for fullName, duplicates := range s.Metadata.FullNames {
		for i, _index := range duplicates.Values {
			if index == _index {
				duplicates.Values = append(duplicates.Values[:i], duplicates.Values[i+1:]...)
				if len(duplicates.Values) == 0 {
					delete(s.Metadata.FullNames, fullName)
				}
				break
			}
		}
	}
	if _, exists := s.Metadata.FullNames[newFullName]; !exists {
		s.Metadata.FullNames[newFullName] = &models.Duplicates{}
	}
	s.Metadata.FullNames[newFullName].Values = append(s.Metadata.FullNames[newFullName].Values, index)
}

func (s *ScraperResource) RemoveMetadata(index int64) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	for designator, duplicates := range s.Metadata.Designators {
		for i, _index := range duplicates.Values {
			if index == _index {
				duplicates.Values = append(duplicates.Values[:i], duplicates.Values[i+1:]...)
				if len(duplicates.Values) == 0 {
					delete(s.Metadata.Designators, designator)
				}
				break
			}
		}
	}

	for fullName, duplicates := range s.Metadata.FullNames {
		for i, _index := range duplicates.Values {
			if index == _index {
				duplicates.Values = append(duplicates.Values[:i], duplicates.Values[i+1:]...)
				if len(duplicates.Values) == 0 {
					delete(s.Metadata.FullNames, fullName)
				}
				break
			}
		}
	}
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

	observersCopy := s.Hub.GetAllObservers(true)
	for index := range observersCopy {
		if !existingIndexes[index] {
			fmt.Printf("deleting observer for resource (%s) %d\n", s.Type, index)
			s.Hub.RemoveObserver(index)
			if err := s.removeFromDatastore(index); err != nil {
				fmt.Printf("error deleting resource (%s) from datastore: %v\n", s.Type, err)
			}

			s.RemoveMetadata(index)
		}
	}

	fmt.Printf("observing resource (%s) with %d observer(s)...\n", s.Type, len(s.Hub.GetAllObservers(false)))
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

	DivisionsScraperResource.UpdateMetadata(designator, fullName, index)

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

	TeachersScraperResource.UpdateMetadata(designator, fullName, index)

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
		FullName:   "",
		Schedule:   &models.Schedule{},
	}

	designator, fullName, err := scrapeRoomTitle(doc)
	if err != nil {
		return nil, fmt.Errorf("error scraping division title: %w", err)
	}
	room.Designator = designator
	room.FullName = fullName

	RoomsScraperResource.UpdateMetadata(designator, fullName, index)

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
	indexes = append(indexes, Config.StaticIndexes.Divisions...)

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

				if !utils.CheckURL(Config.BaseUrl + endpoint) {
					fmt.Printf("error opening division " + Config.BaseUrl + endpoint + "\n")
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
	indexes = append(indexes, Config.StaticIndexes.Teachers...)

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

				if !utils.CheckURL(Config.BaseUrl + endpoint) {
					fmt.Printf("error opening teacher " + Config.BaseUrl + endpoint + "\n")
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
	indexes = append(indexes, Config.StaticIndexes.Rooms...)

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

				if !utils.CheckURL(Config.BaseUrl + endpoint) {
					fmt.Printf("error opening room " + Config.BaseUrl + endpoint + "\n")
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
	TeachersScraperResource = NewScraperResource(TeacherIndexRegex, TeacherResource)
	RoomsScraperResource = NewScraperResource(RoomIndexRegex, RoomResource)

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

	waitForFirstRefresh()

	return nil
}

func Cleanup() {
	fmt.Println("cleaning scraper")
	DivisionsScraperResource.StopHub()
	TeachersScraperResource.StopHub()
	RoomsScraperResource.StopHub()
}
