package main

import (
	"os"
	"os/signal"
	"syscall"

	v1 "smegg.me/goptivum/api/v1"
	"smegg.me/goptivum/common/config"
	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/datastore"
	_ "smegg.me/goptivum/docs" // swagger docs
)

func Cleanup() {
	logger.Info("cleaning up")

	if err := datastore.Close(); err != nil {
		logger.Errorf("failed to close database connection: %v", err)
	}
}

func WaitForTermination() {
	callChan := make(chan os.Signal, 1)
	signal.Notify(callChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	logger.Info("waiting for termination signal")
	<-callChan
	logger.Info("termination signal received")

	Cleanup()
}

func Initialize() {
	if err := config.Initialize(); err != nil {
		logger.Fatalf("failed to initialize config: %v", err)
	}

	logger.Info("initializing main application")

	if err := datastore.Initialize(); err != nil {
		logger.Fatalf("failed to initialize datastore: %v", err)
	}

	errCh := v1.Initialize()
	go func() {
		if err := <-errCh; err != nil {
			logger.Errorf("api error: %v", err)
		}
	}()

	logger.Info("main application initialized successfully")
}

func main() {
	Initialize()
	WaitForTermination()
}
