package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/securisec/xfa/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		var code cmd.ExitCode
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
		fmt.Fprintln(os.Stderr, "Error:", err) // byte-identical to cobra's own print
		os.Exit(1)
	}
}
