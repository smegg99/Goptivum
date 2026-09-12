// main.go

// Command goptivum-server is the daemon that owns identity and all
// application data for one school.
package main

import (
	"os"

	"github.com/smegg99/goptivum/backend/cmd"
)

func main() { os.Exit(cmd.Execute()) }
