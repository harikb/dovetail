package main

import (
	"os"

	"github.com/harikb/dovetail/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
