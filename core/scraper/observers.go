package scraper

import (
	"fmt"
	"strings"
	"time"

	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/observer"

	"github.com/PuerkitoBio/goquery"
)

func ObserveDivisions() {
	fmt.Println("observing divisions")

	newDivisionObserver := func(index uint32) *observer.Observer {
		url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Division, index)
		// So they don't all refresh at the same time
		interval := time.Duration((index+1)/10+15) * time.Second
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
			division, err := ScrapeDivision(index)
			if err != nil {
				fmt.Printf("error scraping division: %v\n", err)
				return
			}

			if err := datastore.SetDivision(division); err != nil {
				fmt.Printf("error saving division: %v\n", err)
				return
			}
		})
	}

	refreshDivisionsObservers := func() {
		existingIndexes := make(map[uint32]bool)
		for _, index := range DivisionsIndexes {
			existingIndexes[index] = true
			if _, exists := DivisionsObservers[index]; !exists {
				DivisionsObservers[index] = newDivisionObserver(index)
				startDivisionObserver(DivisionsObservers[index], index)
			}
		}

		for index := range DivisionsObservers {
			if !existingIndexes[index] {
				DivisionsObservers[index].Stop()
				datastore.DeleteDivision(index)
				delete(DivisionsObservers, index)

				for key, _index := range DivisionsDesignators.Divisions {
					if _index == index {
						delete(DivisionsDesignators.Divisions, key)

						if len(DivisionsDesignators.Divisions) == 0 {
							break
						}
					}
				}
			}
		}

		fmt.Printf("observing divisions with %d observer(s)...\n", len(DivisionsObservers))
	}

	DivisionsListObserver = observer.NewObserver(Config.BaseUrl+Config.Endpoints.DivisionsList, 1*time.Second, func(doc *goquery.Document) string {
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

func ObserveTeachers() {
	fmt.Println("observing teachers")

	newTeacherObserver := func(index uint32) *observer.Observer {
		url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Teacher, index)
		interval := time.Duration((index+1)/10+15) * time.Second
		return observer.NewObserver(url, interval, func(doc *goquery.Document) string {
			var content []string
			doc.Find("table").Each(func(i int, table *goquery.Selection) {
				table.Find("td, th").Each(func(i int, s *goquery.Selection) {
					content = append(content, s.Text())
				})
			})

			return strings.Join(content, " ")
		})
	}

	startTeacherObserver := func(observer *observer.Observer, index uint32) {
		observer.Start(func() {
			teacher, err := ScrapeTeacher(index)
			if err != nil {
				fmt.Printf("error scraping teacher: %v\n", err)
				return
			}

			if err := datastore.SetTeacher(teacher); err != nil {
				fmt.Printf("error saving teacher: %v\n", err)
				return
			}
		})
	}

	refreshTeachersObservers := func() {
		existingIndexes := make(map[uint32]bool)
		for _, index := range TeachersIndexes {
			existingIndexes[index] = true
			if _, exists := TeachersObservers[index]; !exists {
				TeachersObservers[index] = newTeacherObserver(index)
				startTeacherObserver(TeachersObservers[index], index)
			}
		}

		for index := range TeachersObservers {
			if !existingIndexes[index] {
				TeachersObservers[index].Stop()
				datastore.DeleteTeacher(index)
				delete(TeachersObservers, index)

				for key, _index := range TeachersDesignators.Teachers {
					if _index == index {
						delete(TeachersDesignators.Teachers, key)

						if len(TeachersDesignators.Teachers) == 0 {
							break
						}
					}
				}
			}
		}

		fmt.Printf("observing teachers with %d observer(s)...\n", len(TeachersObservers))
	}

	TeachersListObserver = observer.NewObserver(Config.BaseUrl+Config.Endpoints.TeachersList, 1*time.Second, func(doc *goquery.Document) string {
		var hrefs []string
		// body > table > tbody
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			hrefs = append(hrefs, s.Text())
		})
		return strings.Join(hrefs, " ")
	})

	TeachersListObserver.Start(func() {
		fmt.Println("teachers list changed!")
		teachersIndexes, err := ScrapeTeachersIndexes()
		if err != nil {
			fmt.Printf("error scraping teachers	indexes: %v\n", err)
			return
		}
		TeachersIndexes = teachersIndexes
		refreshTeachersObservers()
	})

	refreshTeachersObservers()
}

func ObserveRooms() {
	fmt.Println("observing rooms")

	newRoomObserver := func(index uint32) *observer.Observer {
		url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Room, index)
		interval := time.Duration((index + 1) / 10 + 15) * time.Second
		return observer.NewObserver(url, interval, func(doc *goquery.Document) string {
			var content []string
			doc.Find("table").Each(func(i int, table *goquery.Selection) {
				table.Find("td, th").Each(func(i int, s *goquery.Selection) {
					content = append(content, s.Text())
				})
			})

			return strings.Join(content, " ")
		})
	}

	startRoomObserver := func(observer *observer.Observer, index uint32) {
		observer.Start(func() {
			room, err := ScrapeRoom(index)
			if err != nil {
				fmt.Printf("error scraping room: %v\n", err)
				return
			}

			if err := datastore.SetRoom(room); err != nil {
				fmt.Printf("error saving room: %v\n", err)
				return
			}
		})
	}

	refreshRoomsObservers := func() {
		existingIndexes := make(map[uint32]bool)
		for _, index := range RoomsIndexes {
			existingIndexes[index] = true
			if _, exists := RoomsObservers[index]; !exists {
				RoomsObservers[index] = newRoomObserver(index)
				startRoomObserver(RoomsObservers[index], index)
			}
		}

		for index := range RoomsObservers {
			if !existingIndexes[index] {
				RoomsObservers[index].Stop()
				datastore.DeleteRoom(index)
				delete(RoomsObservers, index)

				for key, _index := range RoomsDesignators.Rooms {
					if _index == index {
						delete(RoomsDesignators.Rooms, key)

						if len(RoomsDesignators.Rooms) == 0 {
							break
						}
					}
				}
			}
		}

		fmt.Printf("observing rooms with %d observer(s)...\n", len(RoomsObservers))
	}

	RoomsListObserver = observer.NewObserver(Config.BaseUrl+Config.Endpoints.RoomsList, 1*time.Second, func(doc *goquery.Document) string {
		var hrefs []string
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			hrefs = append(hrefs, s.Text())
		})
		return strings.Join(hrefs, " ")
	})

	RoomsListObserver.Start(func() {
		fmt.Println("rooms list changed!")
		roomsIndexes, err := ScrapeRoomsIndexes()
		if err != nil {
			fmt.Printf("error scraping rooms indexes: %v\n", err)
			return
		}
		RoomsIndexes = roomsIndexes
		refreshRoomsObservers()
	})

	refreshRoomsObservers()
}