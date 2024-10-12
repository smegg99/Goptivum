// main.go
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/core/scraper"

	"smuggr.xyz/optivum-bsf/api/v1"
)

func main() {
	if err := config.Initialize(); err != nil {
		panic(err)
	}

	if err := scraper.Initialize(); err != nil {
		panic(err)
	}

	errCh := v1.Initialize()
	defer v1.Cleanup()

	<- errCh

	division, err := scraper.ScrapeDivision(4)
	if err != nil {
		panic(err)
	}

	file, err := os.Create("division.json")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(division); err != nil {
		panic(err)
	}

	fmt.Println("Division data has been written to division.json")
}
