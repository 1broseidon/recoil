package main

import (
	"os"

	"github.com/1broseidon/recoil/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(cmd.HandleError(os.Stderr, err))
	}
}
