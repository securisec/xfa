package main

import (
	"os"

	"github.com/securisec/xfa/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
