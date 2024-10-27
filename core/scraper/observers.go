// scraper/observers.go
package scraper

import (
	"fmt"
	"strings"
	"time"

	"smuggr.xyz/optivum-bsf/core/observer"

	"github.com/PuerkitoBio/goquery"
)

func ObserveDivisions(refreshChan *chan int64)  {
	fmt.Println("observing divisions")

	refreshDivisionsObservers := func() {
		DivisionsScraperResource.RefreshObservers()
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
		DivisionsScraperResource.UpdateIndexes(divisionsIndexes)
		refreshDivisionsObservers()
	}

	DivisionsScraperResource.StartHub()

	DivisionsScraperResource.Observer = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.DivisionsList, 1*time.Second, extractFunc, callbackFunc)
	DivisionsScraperResource.Hub.AddObserver(DivisionsScraperResource.Observer)
	refreshDivisionsObservers()
}

func ObserveTeachers(refreshChan *chan int64) {
	fmt.Println("observing teachers")

	refreshTeachersObservers := func() {
		TeachersScraperResource.RefreshObservers()
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
		TeachersScraperResource.UpdateIndexes(teachersIndexes)
		refreshTeachersObservers()
	}

	TeachersScraperResource.StartHub()

	TeachersScraperResource.Observer = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.TeachersList, 1*time.Second, extractFunc, callbackFunc)
	TeachersScraperResource.AddObserver(TeachersScraperResource.Observer)
	refreshTeachersObservers()
}

func ObserveRooms(refreshChan *chan int64) {
	fmt.Println("observing rooms")

	refreshRoomsObservers := func() {
		RoomsScraperResource.RefreshObservers()
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
		RoomsScraperResource.UpdateIndexes(roomsIndexes)
		refreshRoomsObservers()
	}

	RoomsScraperResource.StartHub()

	RoomsScraperResource.Observer = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.RoomsList, 1*time.Second, extractFunc, callbackFunc)
	RoomsScraperResource.Hub.AddObserver(RoomsScraperResource.Observer)
	refreshRoomsObservers()
}