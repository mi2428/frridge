package docker

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func readTerminalState(t *testing.T) *unix.Termios {
	t.Helper()
	state, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
