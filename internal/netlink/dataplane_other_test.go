//go:build !linux

package netlink

import "testing"

func TestLinuxDataplane(t *testing.T) {
	t.Skip("isolated dataplane tests require Linux")
}
