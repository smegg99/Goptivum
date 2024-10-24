// scraper/observers.go
package scraper

import (
	"fmt"
	"strings"
	"time"

	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/observer"

	"github.com/PuerkitoBio/goquery"
)

func newDivisionObserver(index int64, refreshChan chan int64) *observer.Observer {
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

		refreshChan <- index

		if err := datastore.SetDivision(division); err != nil {
			fmt.Printf("error saving division: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Division, index)
	interval := time.Duration((index + 1) / 10 + 15) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newTeacherObserver(index int64, refreshChan chan int64) *observer.Observer {
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

		refreshChan <- index

		if err := datastore.SetTeacher(teacher); err != nil {
			fmt.Printf("error saving teacher: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Teacher, index)
	interval := time.Duration((index + 1) / 10 + 15) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func newRoomObserver(index int64, refreshChan chan int64) *observer.Observer {
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

		refreshChan <- index

		if err := datastore.SetRoom(room); err != nil {
			fmt.Printf("error saving room: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Room, index)
	interval := time.Duration((index + 1) / 10 + 15) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func ObserveDivisions() chan int64 {
	fmt.Println("observing divisions")
	var refreshChan = make(chan int64)

	refreshDivisionsObservers := func() {
		existingIndexes := make(map[int64]bool)
		for _, index := range DivisionsIndexes {
			existingIndexes[index] = true
			if DivisionsHub.GetObserver(index) == nil {
				observer := newDivisionObserver(index, refreshChan)
				DivisionsHub.AddObserver(observer)
			}
		}

		observersCopy := DivisionsHub.GetAllObservers()
		if len(observersCopy) > 0 {
			delete(observersCopy, 0)
		}
		for index := range observersCopy {
			if !existingIndexes[index] {
				DivisionsHub.RemoveObserver(index)
				if err := datastore.DeleteDivision(index); err != nil {
					fmt.Printf("error deleting division: %v\n", err)
				}

				DivisionsMu.Lock()
                for key, _index := range DivisionsDesignators.Designators {
                    if _index == index {
                        delete(DivisionsDesignators.Designators, key)
                        if len(DivisionsDesignators.Designators) == 0 {
                            break
                        }
                    }
                }
                DivisionsMu.Unlock()
			}
		}

		fmt.Printf("observing divisions with %d observer(s)...\n", len(DivisionsHub.GetAllObservers()))
	}

	extractFunc := func(doc *goquery.Document) string {
		var hrefs []string
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	}

	callbackFunc := func() {
		fmt.Println("divisions list changed!")
		divisionsIndexes, err := ScrapeDivisionsIndexes()
		if err != nil {
			fmt.Printf("error scraping divisions indexes: %v\n", err)
			return
		}
		DivisionsIndexes = divisionsIndexes
		refreshDivisionsObservers()
	}

	DivisionsHub.Start()

	DivisionsListObserver = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.DivisionsList, 1*time.Second, extractFunc, callbackFunc)
	DivisionsHub.AddObserver(DivisionsListObserver)
	refreshDivisionsObservers()

	return refreshChan
}

func ObserveTeachers() chan int64 {
	fmt.Println("observing teachers")
	var refreshChan = make(chan int64)

	refreshTeachersObservers := func() {
		existingIndexes := make(map[int64]bool)
		for _, index := range TeachersIndexes {
			existingIndexes[index] = true
			if TeachersHub.GetObserver(index) == nil {
				observer := newTeacherObserver(index, refreshChan)
				TeachersHub.AddObserver(observer)
			}
		}

		teachersCopy := TeachersHub.GetAllObservers()
		if len(teachersCopy) > 0 {
			delete(teachersCopy, 0)
		}
		for index := range teachersCopy {
			if !existingIndexes[index] {
				TeachersHub.RemoveObserver(index)
				if err := datastore.DeleteTeacher(index); err != nil {
					fmt.Printf("error deleting teacher: %v\n", err)
				}

				TeachersMu.Lock()
				for key, _index := range TeachersDesignators.Designators {
					if _index == index {
						delete(TeachersDesignators.Designators, key)

						if len(TeachersDesignators.Designators) == 0 {
							break
						}
					}
				}
				TeachersMu.Unlock()
			}
		}

		fmt.Printf("observing teachers with %d observer(s)...\n", len(TeachersHub.GetAllObservers()))
	}

	extractFunc := func(doc *goquery.Document) string {
		var hrefs []string
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	}

	callbackFunc := func() {
		fmt.Println("teachers list changed!")
		teachersIndexes, err := ScrapeTeachersIndexes()
		if err != nil {
			fmt.Printf("error scraping teachers indexes: %v\n", err)
			return
		}
		TeachersIndexes = teachersIndexes
		refreshTeachersObservers()
	}

	TeachersHub.Start()

	TeachersListObserver = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.TeachersList, 1*time.Second, extractFunc, callbackFunc)
	TeachersHub.AddObserver(TeachersListObserver)
	refreshTeachersObservers()

	return refreshChan
}

func ObserveRooms() chan int64 {
	fmt.Println("observing rooms")
	var refreshChan = make(chan int64)

	refreshRoomsObservers := func() {
		existingIndexes := make(map[int64]bool)
		for _, index := range RoomsIndexes {
			existingIndexes[index] = true
			if RoomsHub.GetObserver(index) == nil {
				observer := newRoomObserver(index, refreshChan)
				RoomsHub.AddObserver(observer)
			}
		}

		roomsCopy := RoomsHub.GetAllObservers()
		if len(roomsCopy) > 0 {
			delete(roomsCopy, 0)
		}
		for index := range roomsCopy {
			if !existingIndexes[index] {
				RoomsHub.RemoveObserver(index)
				if err := datastore.DeleteRoom(index); err != nil {
					fmt.Printf("error deleting room: %v\n", err)
				}

				RoomsMu.Lock()
				for key, _index := range RoomsDesignators.Designators {
					if _index == index {
						delete(RoomsDesignators.Designators, key)

						if len(RoomsDesignators.Designators) == 0 {
							break
						}
					}
				}
				RoomsMu.Unlock()
			}
		}

		fmt.Printf("observing rooms with %d observer(s)...\n", len(RoomsHub.GetAllObservers()))
	}

	extractFunc := func(doc *goquery.Document) string {
		var hrefs []string
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	}

	callbackFunc := func() {
		fmt.Println("rooms list changed!")
		roomsIndexes, err := ScrapeRoomsIndexes()
		if err != nil {
			fmt.Printf("error scraping rooms indexes: %v\n", err)
			return
		}
		RoomsIndexes = roomsIndexes
		refreshRoomsObservers()
	}

	RoomsHub.Start()

	RoomsListObserver = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.RoomsList, 1*time.Second, extractFunc, callbackFunc)
	RoomsHub.AddObserver(RoomsListObserver)
	refreshRoomsObservers()

	return refreshChan
}