package main

import (
	"fmt"
	"os"

	"frridge/internal/multipass"
)

func main() {
	service, err := multipass.NewDefault()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := newRootCommand(service)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
