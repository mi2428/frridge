package multipass

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"
)

var (
	//go:embed cloud-init.yaml.tmpl
	launchCloudInitTemplate string

	//go:embed guest-bootstrap.sh
	guestBootstrapSource string

	launchCloudInitTmpl = template.Must(template.New("cloud-init").Funcs(template.FuncMap{
		"indent": func(prefix, input string) string {
			lines := strings.Split(strings.TrimRight(input, "\n"), "\n")
			for idx, line := range lines {
				lines[idx] = prefix + line
			}
			return strings.Join(lines, "\n")
		},
	}).Parse(launchCloudInitTemplate))
)

// guestBootstrapScript returns the guest-side baseline script used both for
// cloud-init provisioning and later drift repair.
func guestBootstrapScript() string {
	return strings.TrimSpace(guestBootstrapSource) + "\n"
}

// launchCloudInit renders the embedded cloud-init wrapper around the guest
// baseline script. The shell script is the single source of truth for guest
// provisioning logic; cloud-init only installs it and runs it once.
func launchCloudInit() (string, error) {
	var rendered bytes.Buffer
	if err := launchCloudInitTmpl.Execute(&rendered, map[string]string{
		"GuestBootstrap": guestBootstrapScript(),
	}); err != nil {
		return "", fmt.Errorf("render cloud-init template: %w", err)
	}
	return strings.TrimSpace(rendered.String()) + "\n", nil
}

func writeLaunchCloudInitFile() (string, func(), error) {
	cloudInit, err := os.CreateTemp("", "frridge-mp-cloud-init-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("create cloud-init temp file: %w", err)
	}

	content, err := launchCloudInit()
	if err != nil {
		name := cloudInit.Name()
		cloudInit.Close()
		os.Remove(name)
		return "", nil, err
	}

	if _, err := cloudInit.WriteString(content); err != nil {
		name := cloudInit.Name()
		cloudInit.Close()
		os.Remove(name)
		return "", nil, fmt.Errorf("write cloud-init temp file: %w", err)
	}
	if err := cloudInit.Close(); err != nil {
		name := cloudInit.Name()
		os.Remove(name)
		return "", nil, fmt.Errorf("close cloud-init temp file: %w", err)
	}

	name := cloudInit.Name()
	return name, func() { _ = os.Remove(name) }, nil
}
