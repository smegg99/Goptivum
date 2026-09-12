// tools/plaprobe/main.go

// Command plaprobe prints the structure of an Optivum ".pla" without its
// content. A real file is a school's personal data: census and tree disclose
// shape only, and xml is the one command that writes what is inside.
package main

import "os"

func main() { os.Exit(execute()) }
