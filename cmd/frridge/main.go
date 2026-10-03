// Binary frridge is the Linux-native CLI entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	labruntime "frridge/internal/runtime"
	"github.com/spf13/cobra"
)

func main() {
	service, err := labruntime.NewDefaultManager()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := newRootCommand(service)
	if err := executeCommand(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func executeCommand(root *cobra.Command) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return root.ExecuteContext(ctx)
}
