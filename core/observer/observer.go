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

// Observer structure
type Observer struct {
	url            string
	extractContent func(*goquery.Document) string // Function to extract content from the page
	hash           string
	ticker         *time.Ticker
}

// hashContent computes the MD5 hash of the given content
func hashContent(content string) string {
	hasher := md5.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

// NewObserver initializes a new Observer instance
func NewObserver(url string, interval time.Duration, extractFunc func(*goquery.Document) string) *Observer {
	return &Observer{
		url:            url,
		extractContent: extractFunc,
		ticker:         time.NewTicker(interval),
	}
}

// fetchContent retrieves the entire HTML document from the given URL
func (o *Observer) fetchContent() (*goquery.Document, error) {
	// Perform the HTTP request to get the HTML page
	resp, err := http.Get(o.url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the page: %v", err)
	}
	defer resp.Body.Close()

	// Parse the HTML with GoQuery
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the HTML: %v", err)
	}

	return doc, nil
}

// compareHash checks if the content's hash has changed and updates the hash if it has
func (o *Observer) compareHash() bool {
	// Fetch the current HTML document
	doc, err := o.fetchContent()
	if err != nil {
		fmt.Printf("Error fetching content: %v\n", err)
		return false
	}

	// Use the user-provided function to extract content to be hashed
	content := o.extractContent(doc)

	// Compute the hash of the content
	hash := hashContent(content)

	// Compare the new hash with the old hash
	if hash != o.hash {
		o.hash = hash
		return true
	}

	return false
}

// Start begins observing the content and triggers the callback if a change is detected
func (o *Observer) Start(callback func()) {
	go func() {
		for range o.ticker.C {
			if o.compareHash() {
				// Call the callback function when a change is detected
				callback()
			}
		}
	}()
}

// Stop halts the observation process
func (o *Observer) Stop() {
	o.ticker.Stop()
}
