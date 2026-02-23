package main

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/cmd"
)

var (
	version   = "dev"
	buildDate = "unknown"
)

func main() {
	if err := cmd.Execute(version, buildDate); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
