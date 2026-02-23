package main

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/cmd"
)

var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
