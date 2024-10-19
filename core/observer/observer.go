// observer.go
package observer

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Observer struct {
	url            string
	extractContent func(*goquery.Document) string
	hash           string
	ticker         *time.Ticker
}

func hashContent(content string) string {
	hasher := md5.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

func NewObserver(url string, interval time.Duration, extractFunc func(*goquery.Document) string) *Observer {
	return &Observer{
		url:            url,
		extractContent: extractFunc,
		ticker:         time.NewTicker(interval),
	}
}

func (o *Observer) fetchContent() (*goquery.Document, error) {
	resp, err := http.Get(o.url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the page: %v", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the HTML: %v", err)
	}

	return doc, nil
}

func (o *Observer) compareHash() bool {
	doc, err := o.fetchContent()
	if err != nil {
		fmt.Printf("error fetching content: %v\n", err)
		return false
	}

	content := o.extractContent(doc)
	hash := hashContent(content)

	if hash != o.hash {
		o.hash = hash
		return true
	}

	return false
}

func (o *Observer) Start(callback func()) {
	check := func() {
		fmt.Println("checking for updates: " + o.url)
		if o.compareHash() {
			fmt.Println("content has changed: " + o.url)
			callback()
		}
	}

	go func() {
		check()
		for range o.ticker.C {
			check()
		}
	}()
}

func (o *Observer) Stop() {
	o.ticker.Stop()
}
