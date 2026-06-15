package multipass

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
)

// ErrInstanceNotFound reports that the requested Multipass instance does not
// exist yet.
var ErrInstanceNotFound = errors.New("multipass instance not found")

type systemCLI struct{}

func newSystemCLI() *systemCLI {
	return &systemCLI{}
}

func (c *systemCLI) Info(ctx context.Context, instance string) (Info, error) {
	stdout, stderr, err := c.output(ctx, "info", "--format", "json", instance)
	if err != nil {
		if looksLikeInstanceNotFound(stderr) {
			return Info{}, ErrInstanceNotFound
		}
		return Info{}, commandError("multipass info", err, stderr)
	}

	type mountInfo struct {
		SourcePath string `json:"source_path"`
	}
	type instanceInfo struct {
		State  string               `json:"state"`
		Mounts map[string]mountInfo `json:"mounts"`
	}
	var payload struct {
		Info map[string]instanceInfo `json:"info"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return Info{}, fmt.Errorf("decode multipass info: %w", err)
	}

	entry, ok := payload.Info[instance]
	if !ok {
		return Info{}, ErrInstanceNotFound
	}

	mounts := make(map[string]string, len(entry.Mounts))
	for target, mount := range entry.Mounts {
		mounts[target] = mount.SourcePath
	}

	return Info{
		State:  entry.State,
		Mounts: mounts,
	}, nil
}

func (c *systemCLI) Launch(ctx context.Context, spec Instance) error {
	args := []string{
		"launch",
		spec.Image,
		"--name", spec.Name,
		"--cpus", fmt.Sprintf("%d", spec.CPUs),
		"--memory", spec.Memory,
		"--disk", spec.Disk,
	}
	_, stderr, err := c.output(ctx, args...)
	if err != nil {
		return commandError("multipass launch", err, stderr)
	}
	return nil
}

func (c *systemCLI) Start(ctx context.Context, instance string) error {
	_, stderr, err := c.output(ctx, "start", instance)
	if err != nil {
		return commandError("multipass start", err, stderr)
	}
	return nil
}

func (c *systemCLI) Mount(ctx context.Context, source, target string) error {
	_, stderr, err := c.output(ctx, "mount", source, target)
	if err != nil {
		return commandError("multipass mount", err, stderr)
	}
	return nil
}

func (c *systemCLI) Transfer(ctx context.Context, source, target string) error {
	_, stderr, err := c.output(ctx, "transfer", source, target)
	if err != nil {
		return commandError("multipass transfer", err, stderr)
	}
	return nil
}

func (c *systemCLI) Exec(ctx context.Context, instance string, spec ExecSpec) error {
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "multipass", buildExecArgs(instance, spec)...)
	cmd.Stdin = spec.Stdin
	if spec.Stdout != nil {
		cmd.Stdout = spec.Stdout
	} else {
		cmd.Stdout = io.Discard
	}
	if spec.Stderr != nil {
		cmd.Stderr = io.MultiWriter(spec.Stderr, &stderr)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		return commandError("multipass exec", err, stderr.String())
	}
	return nil
}

func (c *systemCLI) Output(ctx context.Context, instance string, spec ExecSpec) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "multipass", buildExecArgs(instance, spec)...)
	cmd.Stdin = spec.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", commandError("multipass exec", err, stderr.String())
	}
	return stdout.String(), nil
}

func (c *systemCLI) output(ctx context.Context, args ...string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "multipass", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func buildExecArgs(instance string, spec ExecSpec) []string {
	command := append([]string(nil), spec.Command...)
	if spec.Dir == "" && len(spec.Env) == 0 {
		return append([]string{"exec", instance, "--"}, command...)
	}
	if spec.Dir == "" {
		spec.Dir = "."
	}

	exports := sortedEnvPairs(spec.Env)
	args := []string{
		"exec",
		instance,
		"--",
		"bash",
		"-lc",
		`set -euo pipefail
cd "$1"
shift
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
	export "$1"
	shift
done
shift
exec "$@"`,
		"bash",
		spec.Dir,
	}
	args = append(args, exports...)
	args = append(args, "--")
	args = append(args, command...)
	return args
}

func sortedEnvPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+env[key])
	}
	return pairs
}

func looksLikeInstanceNotFound(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "does not exist") || strings.Contains(lower, "not found")
}

func commandError(prefix string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %w: %s", prefix, err, stderr)
}
