package main

import (
	"fmt"
	"os"

	"frridge/internal/app"
	labruntime "frridge/internal/runtime"
)

func main() {
	service, err := labruntime.NewDefaultManager()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := app.NewRootCommand(service)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
