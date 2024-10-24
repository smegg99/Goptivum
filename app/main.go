// app/main.go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/scraper"
	"smuggr.xyz/optivum-bsf/api/v1"
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
	datastore.Cleanup();
}

func main() {
	if err := config.Initialize(); err != nil {
		panic(err)
	}

	if err := datastore.Initialize(); err != nil {
		panic(err)
	}

	scheduleChannels, err := scraper.Initialize()
	if err != nil {
		panic(err)
	}

	v1.Initialize(scheduleChannels)
	
	defer Cleanup()

	WaitForTermination()
}