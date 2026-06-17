package multipass

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// ErrInstanceNotFound reports that the requested Multipass instance does not
// exist yet.
var ErrInstanceNotFound = errors.New("multipass instance not found")

const (
	outputMarkerPrefix = "__FRRIDGE_OUTPUT_BEGIN__"
	statusMarkerPrefix = "__FRRIDGE_EXIT_STATUS__="
)

type systemCLI struct{}

type shellMarkers struct {
	output string
	status string
}

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
	cloudInitPath, cleanup, err := writeLaunchCloudInitFile()
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{
		"launch",
		spec.Image,
		"--name", spec.Name,
		"--cpus", fmt.Sprintf("%d", spec.CPUs),
		"--memory", spec.Memory,
		"--disk", spec.Disk,
		"--cloud-init", cloudInitPath,
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
	if spec.Stdin == nil {
		return c.execBuffered(ctx, instance, spec)
	}

	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "multipass", "shell", instance)
	cmd.Stdin = shellInput(spec, nil)
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
		return commandError("multipass shell", err, stderr.String())
	}
	return nil
}

func (c *systemCLI) Output(ctx context.Context, instance string, spec ExecSpec) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	markers, err := newShellMarkers()
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "multipass", "shell", instance)
	cmd.Stdin = shellInput(spec, &markers)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", commandError("multipass shell", err, stderr.String())
	}

	output, exitCode, err := parseShellOutput(stdout.String(), markers)
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", commandError("multipass shell", fmt.Errorf("guest command exited with status %d", exitCode), stderr.String())
	}
	return output, nil
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

func (c *systemCLI) execBuffered(ctx context.Context, instance string, spec ExecSpec) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	markers, err := newShellMarkers()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "multipass", "shell", instance)
	cmd.Stdin = shellInput(spec, &markers)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError("multipass shell", err, stderr.String())
	}

	output, exitCode, err := parseShellOutput(stdout.String(), markers)
	if err != nil {
		return err
	}
	stderrText := stderr.String()
	if spec.Stdout != nil && output != "" {
		if _, err := io.WriteString(spec.Stdout, output); err != nil {
			return fmt.Errorf("write multipass stdout: %w", err)
		}
	}
	if spec.Stderr != nil && stderr.Len() > 0 {
		if _, err := io.Copy(spec.Stderr, &stderr); err != nil {
			return fmt.Errorf("write multipass stderr: %w", err)
		}
	}
	if exitCode != 0 {
		return commandError("multipass shell", fmt.Errorf("guest command exited with status %d", exitCode), stderrText)
	}
	return nil
}

func newShellMarkers() (shellMarkers, error) {
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return shellMarkers{}, fmt.Errorf("generate shell markers: %w", err)
	}
	suffix := hex.EncodeToString(token[:])
	return shellMarkers{
		output: outputMarkerPrefix + suffix,
		status: statusMarkerPrefix + suffix + "=",
	}, nil
}

func shellInput(spec ExecSpec, markers *shellMarkers) io.Reader {
	script := buildShellScript(spec, markers)
	if spec.Stdin == nil {
		return strings.NewReader(script)
	}
	return io.MultiReader(strings.NewReader(script), spec.Stdin)
}

func buildShellScript(spec ExecSpec, markers *shellMarkers) string {
	var builder strings.Builder
	builder.WriteString("set -euo pipefail\n")
	if strings.TrimSpace(spec.Dir) != "" {
		builder.WriteString("cd ")
		builder.WriteString(shellQuote(spec.Dir))
		builder.WriteByte('\n')
	}
	for _, key := range sortedEnvKeys(spec.Env) {
		builder.WriteString("export ")
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(shellQuote(spec.Env[key]))
		builder.WriteByte('\n')
	}
	if markers != nil {
		builder.WriteString("printf ")
		builder.WriteString(shellQuote(markers.output + "\\n"))
		builder.WriteByte('\n')
	}
	builder.WriteString("set +e\n")
	builder.WriteString(shellJoin(spec.Command))
	builder.WriteString("\nstatus=$?\n")
	if markers != nil {
		builder.WriteString("printf ")
		builder.WriteString(shellQuote(markers.status + "%s\\n"))
		builder.WriteString(" \"$status\"\n")
		builder.WriteString("exit 0\n")
		return builder.String()
	}
	builder.WriteString("exit \"$status\"\n")
	return builder.String()
}

func sortedEnvKeys(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func stripOutputBanner(output string, markers shellMarkers) string {
	_, rest, ok := strings.Cut(output, markers.output+"\n")
	if ok {
		return rest
	}
	return output
}

func parseShellOutput(output string, markers shellMarkers) (string, int, error) {
	output = stripOutputBanner(output, markers)
	index := strings.LastIndex(output, markers.status)
	if index == -1 {
		return output, 0, nil
	}

	content := output[:index]
	statusText := output[index+len(markers.status):]
	statusText = strings.TrimSpace(statusText)
	exitCode, err := strconv.Atoi(statusText)
	if err != nil {
		return "", 0, fmt.Errorf("parse multipass guest exit status %q: %w", statusText, err)
	}
	return content, exitCode, nil
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
