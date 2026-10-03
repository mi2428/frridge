package docker

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func readTerminalState(t *testing.T) *unix.Termios {
	t.Helper()
	state, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TIOCGETA)
	if err != nil {
		t.Fatal(err)
	}
	// PENDIN is transient kernel input bookkeeping, not a terminal setting.
	state.Lflag &^= unix.PENDIN
	return state
}
