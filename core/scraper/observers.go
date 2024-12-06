// scraper/observers.go
package scraper

import (
	"fmt"
	"strings"
	"time"

	"smuggr.xyz/goptivum/core/observer"

	"github.com/PuerkitoBio/goquery"
)

func ObserveDivisions() {
	fmt.Println("observing divisions")

	refreshDivisionsObservers := func() {
		DivisionsScraperResource.RefreshObservers()
	}

	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var hrefs []string
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		fmt.Println("divisions list changed!")
		divisionsIndexes, err := ScrapeDivisionsIndexes()
		if err != nil {
			fmt.Printf("error scraping divisions indexes: %v\n", err)
			return
		}
		DivisionsScraperResource.UpdateIndexes(divisionsIndexes)
		refreshDivisionsObservers()
	}

	DivisionsScraperResource.StartHub()

	DivisionsScraperResource.Observer = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.DivisionsList, 1*time.Second, extractFunc, callbackFunc)
	DivisionsScraperResource.Hub.AddObserver(DivisionsScraperResource.Observer)
	refreshDivisionsObservers()
}

func ObserveTeachers() {
	fmt.Println("observing teachers")

	refreshTeachersObservers := func() {
		TeachersScraperResource.RefreshObservers()
	}

	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var hrefs []string
		doc.Find("table a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		fmt.Println("teachers list changed!")
		teachersIndexes, err := ScrapeTeachersIndexes()
		if err != nil {
			fmt.Printf("error scraping teachers indexes: %v\n", err)
			return
		}
		TeachersScraperResource.UpdateIndexes(teachersIndexes)
		refreshTeachersObservers()
	}

	TeachersScraperResource.StartHub()

	TeachersScraperResource.Observer = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.TeachersList, 1*time.Second, extractFunc, callbackFunc)
	TeachersScraperResource.AddObserver(TeachersScraperResource.Observer)
	refreshTeachersObservers()
}

func ObserveRooms() {
	fmt.Println("observing rooms")

	refreshRoomsObservers := func() {
		RoomsScraperResource.RefreshObservers()
	}

	extractFunc := func(o *observer.Observer, doc *goquery.Document) string {
		var hrefs []string
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists {
				hrefs = append(hrefs, href)
			}
		})
		return strings.Join(hrefs, " ")
	}

	callbackFunc := func(o *observer.Observer) {
		fmt.Println("rooms list changed!")
		roomsIndexes, err := ScrapeRoomsIndexes()
		if err != nil {
			fmt.Printf("error scraping rooms indexes: %v\n", err)
			return
		}
		RoomsScraperResource.UpdateIndexes(roomsIndexes)
		refreshRoomsObservers()
	}

	RoomsScraperResource.StartHub()

	RoomsScraperResource.Observer = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.RoomsList, 1*time.Second, extractFunc, callbackFunc)
	RoomsScraperResource.Hub.AddObserver(RoomsScraperResource.Observer)
	refreshRoomsObservers()
}

func ObserveTeachersOnDuty() {
	fmt.Println("observing teachers on duty")

	TeachersOnDutyScraperResource.StartHub()

	TeachersOnDutyScraperResource.Observer = TeachersOnDutyScraperResource.newObserver(0)
	TeachersOnDutyScraperResource.Hub.AddObserver(TeachersOnDutyScraperResource.Observer)
}

func ObservePractices() {
	fmt.Println("observing practices")

	PracticesScraperResource.StartHub()

	PracticesScraperResource.Observer = PracticesScraperResource.newObserver(0)
	PracticesScraperResource.Hub.AddObserver(PracticesScraperResource.Observer)
}