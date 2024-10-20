// utils/utils.go
package utils

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

func CheckURL(url string) bool {
	// #nosec G107
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func IsEmptyOrInvisible(text string) bool {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.TrimSpace(text)

	return text == ""
}

func OpenDoc(baseUrl, endpoint string) (*goquery.Document, error) {
	url := fmt.Sprintf("%s%s", baseUrl, endpoint)
	fmt.Printf("fetching URL: %s\n", url)
	
	// #nosec G107
	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching URL: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, fmt.Errorf("error loading HTML: %w", err)
	}

	return doc, nil
}

func MergeChans(channels ...chan string) chan string {
    var wg sync.WaitGroup
    out := make(chan string)

    multiplex := func(c chan string) {
        defer wg.Done()
        for msg := range c {
            out <- msg
        }
    }

    wg.Add(len(channels))
    for _, ch := range channels {
        go multiplex(ch)
    }

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}