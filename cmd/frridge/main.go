// Binary frridge is the Linux-native CLI entry point.
package main

import (
	"fmt"
	"os"

	labruntime "frridge/internal/runtime"
)

func main() {
	service, err := labruntime.NewDefaultManager()
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
