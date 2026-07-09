// main.go

package main

import (
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/smegg99/arrango/backend/api"
	"github.com/smegg99/arrango/backend/config"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/arrango/backend/jobs"
)

func main() {
	cfg := config.Load()

	conn, err := grpc.NewClient(cfg.SolverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect solver: %v", err)
	}
	defer conn.Close()

	server := api.NewServer(arrangov1.NewSolverServiceClient(conn),
		jobs.NewRegistry())
	log.Printf("arrango backend listening on %s (solver %s)",
		cfg.HTTPAddr, cfg.SolverAddr)
	if err := server.Router().Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("http server: %v", err)
	}
}
