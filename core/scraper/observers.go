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

	DivisionsObservers = make(map[uint32]*observer.Observer)

	newDivisionObserver := func(index uint32) *observer.Observer {
		url := fmt.Sprintf(Config.BaseUrl+Config.Endpoints.Division, index)
		// So they don't all refresh at the same time
		interval := time.Duration((index+1)/10 + 15) * time.Second 
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
			}
		}

		fmt.Printf("observing divisions with %d observer(s)...\n", len(DivisionsObservers))
	}

	DivisionsListObserver = observer.NewObserver(Config.BaseUrl+Config.Endpoints.DivisionsList, time.Second * 10, func(doc *goquery.Document) string {
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
