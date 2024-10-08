// main.go
package main

import (
	"fmt"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/core/scraper"
)

func main() {
	if err := config.Initialize(); err != nil {
		panic(err)
	}

	if err := scraper.Initialize(); err != nil {
		panic(err)
	}

	division, err := scraper.ScrapeDivision(4)
	if err != nil {
		panic(err)
	}

	fmt.Println(division.Schedule.String())
	
	// teacher, err := scraper.ScrapeTeacher(1)
	// if err != nil {
	// 	panic(err)
	// }

	// fmt.Println(teacher)

	// room, err := scraper.ScrapeRoom(1)
	// if err != nil {
	// 	panic(err)
	// }

	// fmt.Println(room)
}
