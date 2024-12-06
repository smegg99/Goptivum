// observer/observer.go
package observer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Observer struct {
	Index 		   int64
	URL            string
	ExtractContent func(*Observer, *goquery.Document) string
	Hash           string
	Interval       time.Duration
	Callback       func(*Observer)
	NextRun        time.Time
	FirstRun       bool
	Mu 		       sync.RWMutex
}

func hashContent(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

func NewObserver(index int64, url string, interval time.Duration, extractFunc func(*Observer, *goquery.Document) string, callbackFunc func(*Observer)) *Observer {
	return &Observer{
		URL:            url,
		Index:          index,
		ExtractContent: extractFunc,
		Callback: 	    callbackFunc,
		Interval:       interval,
		NextRun:        time.Now(),
		FirstRun:       true,
		Mu: 		    sync.RWMutex{},
	}
}

func (o *Observer) fetchContent(ctx context.Context, client *http.Client) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := client.Do(req)
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

func (o *Observer) compareHash(ctx context.Context, client *http.Client) (bool, error) {
	o.Mu.RLock()
	defer o.Mu.RUnlock()

	var doc *goquery.Document
	var err error

	for i := 0; i < 3; i++ {
		doc, err = o.fetchContent(ctx, client)
		if err == nil {
			break
		}

		// TODO: Cancel that on signal received
		fmt.Printf("error fetching content from %s, retrying... (%d/3)\n", o.URL, i+1)
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		return false, fmt.Errorf("error fetching content from %s: %v", o.URL, err)
	}

	content := o.ExtractContent(o, doc)
	hash := hashContent(content)

	if hash != o.Hash {
		o.Hash = hash
		return true, nil
	}

	return false, nil
}

// Helper method to integrate with the Hub's worker pool.
func (o *Observer) CompareHashWithClient(ctx context.Context, client *http.Client) bool {
	changed, err := o.compareHash(ctx, client)
	if err != nil {
		fmt.Println(err)
		return false
	}
	return changed
}