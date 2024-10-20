// scraper/observers.go
package scraper

import (
	"fmt"
	"strings"
	"time"

	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/hub"
	"smuggr.xyz/optivum-bsf/core/observer"

	"github.com/PuerkitoBio/goquery"
)

func newDivisionObserver(index int64, refreshChan chan string) *observer.Observer {
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

		return strings.Join(content, " ")
	}

	callbackFunc := func() {
		division, err := ScrapeDivision(index)
		if err != nil {
			fmt.Printf("error scraping division: %v\n", err)
			return
		}

		refreshChan <- "division"

		if err := datastore.SetDivision(division); err != nil {
			fmt.Printf("error saving division: %v\n", err)
			return
		}
	}

	url := fmt.Sprintf(Config.BaseUrl + Config.Endpoints.Division, index)
	// So they don't all refresh at the same time
	interval := time.Duration((index + 1) / 10 + 15) * time.Second

	return observer.NewObserver(index, url, interval, extractFunc, callbackFunc)
}

func ObserveDivisions() chan string {
	fmt.Println("observing divisions")
	var refreshChan = make(chan string)

	refreshDivisionsObservers := func() {
		existingIndexes := make(map[int64]bool)
		for _, index := range DivisionsIndexes {
			existingIndexes[index] = true
			if DivisionsHub.GetObserver(index) == nil {
				observer := newDivisionObserver(index, refreshChan)
				DivisionsHub.AddObserver(observer)
			}
		}

		for index := range DivisionsHub.GetAllObservers() {
			if !existingIndexes[index] {
				DivisionsHub.RemoveObserver(index)
				if err := datastore.DeleteDivision(index); err != nil {
					fmt.Printf("error deleting division: %v\n", err)
				}

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

	DivisionsHub = hub.NewHub(10)
	DivisionsHub.Start()

	DivisionsListObserver = observer.NewObserver(0, Config.BaseUrl+Config.Endpoints.DivisionsList, 1*time.Second, extractFunc, callbackFunc)
	DivisionsHub.AddObserver(DivisionsListObserver)
	refreshDivisionsObservers()

	return refreshChan
}