// app/main.go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	v1 "smuggr.xyz/goptivum/api/v1"
	"smuggr.xyz/goptivum/common/config"
	"smuggr.xyz/goptivum/common/models"
	"smuggr.xyz/goptivum/common/utils"
	"smuggr.xyz/goptivum/core/datastore"
	"smuggr.xyz/goptivum/core/scraper"
)

func WaitForTermination() {
	callChan := make(chan os.Signal, 1)
	signal.Notify(callChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	fmt.Println("waiting for termination signal...")
	<-callChan
	fmt.Println("termination signal received")
}

func Cleanup() {
	fmt.Println("cleaning up...")

	scraper.Cleanup()
	datastore.Cleanup()
}

func main() {
	if err := config.Initialize(); err != nil {
		panic(err)
	}

	utils.Initialize()

	if err := datastore.Initialize(); err != nil {
		panic(err)
	}

	err := scraper.Initialize()
	if err != nil {
		panic(err)
	}

	v1.Initialize(&models.ScheduleChannels{
		Divisons: scraper.DivisionsScraperResource.RefreshChan,
		Teachers: scraper.TeachersScraperResource.RefreshChan,
		Rooms:    scraper.RoomsScraperResource.RefreshChan,
		Duties:   scraper.TeachersOnDutyScraperResource.RefreshChan,
	})

	defer Cleanup()

	WaitForTermination()
}
