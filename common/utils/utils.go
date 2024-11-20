// utils/utils.go
package utils

import (
	"fmt"
	"net/http"
	"crypto/tls"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"smuggr.xyz/goptivum/common/config"
)

var ScraperConfig config.ScraperConfig
var HttpClient *http.Client

func Initialize() {
	fmt.Println("initializing utils")
	ScraperConfig = config.Global.Scraper

	HttpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:    &tls.Config{
				InsecureSkipVerify: ScraperConfig.IgnoreCertificates,
			},
		},
		Timeout: 20 * time.Second,
	}
}

func CheckURL(url string) bool {
	fmt.Println("checking URL:", url)

	// #nosec G107
	resp, err := HttpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	fmt.Printf("got status code: %d for %s\n", resp.StatusCode, url)

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

	var res *http.Response
	var err error

	for i := 0; i < 3; i++ {
		// #nosec G107
		res, err = HttpClient.Get(url)
		if err == nil && res.StatusCode == http.StatusOK {
			break
		}
		if res != nil {
			if err := res.Body.Close(); err != nil {
				return nil, fmt.Errorf("error closing response body: %w", err)
			}
		}
		time.Sleep(1 * time.Second)
	}

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