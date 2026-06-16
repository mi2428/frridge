package multipass

import (
	"strings"
	"testing"
)

func TestLaunchCloudInitProvisioning(t *testing.T) {
	t.Parallel()

	userData, err := launchCloudInit()
	if err != nil {
		t.Fatalf("launchCloudInit() error = %v", err)
	}
	for _, want := range []string{
		"#cloud-config",
		"/usr/local/lib/frridge-mp/guest-bootstrap.sh",
		"content: |",
		"runcmd:",
		"  - [/usr/local/lib/frridge-mp/guest-bootstrap.sh]",
	} {
		if !strings.Contains(userData, want) {
			t.Fatalf("launchCloudInit() missing %q in %q", want, userData)
		}
	}
}

func TestGuestBootstrapScriptCarriesProvisioningLogic(t *testing.T) {
	t.Parallel()

	script := guestBootstrapScript()
	for _, want := range []string{
		"#!/bin/bash",
		`mods_pkg="linux-modules-extra-$(uname -r)"`,
		"apt-get install -y",
		"systemctl enable --now docker",
		"/etc/modules-load.d/frridge-mp.conf",
		"modprobe \"$module\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("guestBootstrapScript() missing %q in %q", want, script)
		}
	}
}
