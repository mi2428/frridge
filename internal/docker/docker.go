// Package docker wraps the Docker SDK behind the smaller surface that frridge
// needs for lab lifecycle management.
package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	containerapi "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	imageapi "github.com/docker/docker/api/types/image"
	mountapi "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/term"
)

// Client is the Docker surface used by the runtime manager.
type Client interface {
	CreateContainer(ctx context.Context, spec ContainerSpec) (string, error)
	StartContainer(ctx context.Context, id string) error
	InspectContainer(ctx context.Context, id string) (Inspect, error)
	ListContainers(ctx context.Context, labels map[string]string) ([]string, error)
	RemoveContainer(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, cmd []string) (ExecResult, error)
	ExecInteractive(ctx context.Context, id string, cmd []string) error
}

// ContainerSpec describes one lab router container before creation.
type ContainerSpec struct {
	Name       string
	Image      string
	Hostname   string
	Env        map[string]string
	Labels     map[string]string
	Sysctls    map[string]string
	Mounts     []Mount
	Privileged bool
}

// Mount describes one bind mount inside a container.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// Inspect exposes only the container state that the runtime needs later.
type Inspect struct {
	ID  string
	PID int
}

// ExecResult captures the output and exit status of a non-interactive exec.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SDKClient implements Client using github.com/docker/docker.
type SDKClient struct {
	api *client.Client
}

// New constructs a Docker SDK client configured from the current environment.
func New() (*SDKClient, error) {
	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &SDKClient{api: api}, nil
}

func (c *SDKClient) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	if err := c.ensureImage(ctx, spec.Image); err != nil {
		return "", err
	}

	response, err := c.api.ContainerCreate(
		ctx,
		&containerapi.Config{
			Image:    spec.Image,
			Hostname: spec.Hostname,
			Env:      envPairs(spec.Env),
			Labels:   copyStringMap(spec.Labels),
		},
		&containerapi.HostConfig{
			NetworkMode: "none",
			Privileged:  spec.Privileged,
			Sysctls:     copyStringMap(spec.Sysctls),
			Mounts:      toAPIMounts(spec.Mounts),
		},
		nil,
		nil,
		spec.Name,
	)
	if err != nil {
		return "", fmt.Errorf("docker create failed: %w", err)
	}

	return response.ID, nil
}

func (c *SDKClient) StartContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, containerapi.StartOptions{}); err != nil {
		return fmt.Errorf("docker start failed: %w", err)
	}
	return nil
}

func (c *SDKClient) InspectContainer(ctx context.Context, id string) (Inspect, error) {
	inspect, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return Inspect{}, fmt.Errorf("docker inspect failed: %w", err)
	}

	return Inspect{
		ID:  inspect.ID,
		PID: inspect.State.Pid,
	}, nil
}

func (c *SDKClient) ListContainers(ctx context.Context, labels map[string]string) ([]string, error) {
	containers, err := c.api.ContainerList(ctx, containerapi.ListOptions{
		All:     true,
		Filters: labelFilters(labels),
	})
	if err != nil {
		return nil, fmt.Errorf("docker list containers failed: %w", err)
	}

	ids := make([]string, 0, len(containers))
	for _, container := range containers {
		ids = append(ids, container.ID)
	}
	return ids, nil
}

func (c *SDKClient) RemoveContainer(ctx context.Context, id string) error {
	err := c.api.ContainerRemove(ctx, id, containerapi.RemoveOptions{
		Force: true,
	})
	if err == nil || errdefs.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("docker remove failed: %w", err)
}

func (c *SDKClient) Exec(ctx context.Context, id string, cmd []string) (ExecResult, error) {
	execID, err := c.createExec(ctx, id, cmd, false)
	if err != nil {
		return ExecResult{}, err
	}

	attach, err := c.api.ContainerExecAttach(ctx, execID, containerapi.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("docker exec attach failed: %w", err)
	}
	defer attach.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := copyExecOutput(&stdout, &stderr, attach.Reader, false); err != nil {
		return ExecResult{}, fmt.Errorf("docker exec output copy failed: %w", err)
	}

	inspect, err := c.api.ContainerExecInspect(ctx, execID)
	if err != nil {
		return ExecResult{}, fmt.Errorf("docker exec inspect failed: %w", err)
	}

	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: inspect.ExitCode,
	}, nil
}

func (c *SDKClient) ExecInteractive(ctx context.Context, id string, cmd []string) error {
	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	// Docker only needs a TTY on the container side. Keeping TTY enabled when
	// stdout is interactive but stdin is piped makes console wrappers such as
	// script(1) behave like docker exec -it.
	tty := term.IsTerminal(int(os.Stdout.Fd()))

	execID, err := c.createExec(ctx, id, cmd, tty)
	if err != nil {
		return err
	}

	attach, err := c.api.ContainerExecAttach(ctx, execID, containerapi.ExecAttachOptions{
		Tty: tty,
	})
	if err != nil {
		return fmt.Errorf("docker exec attach failed: %w", err)
	}
	defer attach.Close()

	if tty && stdinTTY {
		restore, err := makeRawTerminal()
		if err != nil {
			return err
		}
		defer restore()

		if err := c.attachResize(ctx, execID); err != nil {
			return err
		}
	}

	go func() {
		_, _ = io.Copy(attach.Conn, os.Stdin)
		_ = attach.CloseWrite()
	}()

	if err := copyExecOutput(os.Stdout, os.Stderr, attach.Reader, tty); err != nil {
		return fmt.Errorf("docker exec output copy failed: %w", err)
	}

	inspect, err := c.api.ContainerExecInspect(ctx, execID)
	if err != nil {
		return fmt.Errorf("docker exec inspect failed: %w", err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("docker exec interactive exit code %d", inspect.ExitCode)
	}
	return nil
}

func (c *SDKClient) ensureImage(ctx context.Context, image string) error {
	if strings.TrimSpace(image) == "" {
		return fmt.Errorf("container image must not be empty")
	}

	_, err := c.api.ImageInspect(ctx, image)
	if err == nil {
		return nil
	}
	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("docker inspect image failed: %w", err)
	}

	reader, err := c.api.ImagePull(ctx, image, imageapi.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker pull failed: %w", err)
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("read docker pull response: %w", err)
	}
	return nil
}

func (c *SDKClient) createExec(ctx context.Context, id string, cmd []string, tty bool) (string, error) {
	response, err := c.api.ContainerExecCreate(ctx, id, containerapi.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
		Tty:          tty,
	})
	if err != nil {
		return "", fmt.Errorf("docker exec create failed: %w", err)
	}
	return response.ID, nil
}

// attachResize keeps the Docker exec PTY aligned with the user's current
// terminal size so fullscreen terminal apps and line editing behave normally.
func (c *SDKClient) attachResize(ctx context.Context, execID string) error {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return fmt.Errorf("get terminal size: %w", err)
	}
	if err := c.api.ContainerExecResize(ctx, execID, containerapi.ResizeOptions{
		Width:  uint(width),
		Height: uint(height),
	}); err != nil {
		return fmt.Errorf("resize docker exec tty: %w", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(signals)
		for {
			select {
			case <-ctx.Done():
				return
			case <-signals:
				width, height, err := term.GetSize(int(os.Stdout.Fd()))
				if err == nil {
					_ = c.api.ContainerExecResize(context.Background(), execID, containerapi.ResizeOptions{
						Width:  uint(width),
						Height: uint(height),
					})
				}
			}
		}
	}()

	return nil
}

func copyExecOutput(stdout io.Writer, stderr io.Writer, src io.Reader, tty bool) error {
	if tty {
		_, err := io.Copy(stdout, src)
		return normalizeCopyError(err)
	}
	_, err := stdcopy.StdCopy(stdout, stderr, src)
	return normalizeCopyError(err)
}

func normalizeCopyError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func makeRawTerminal() (func(), error) {
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("set raw terminal mode: %w", err)
	}
	return func() {
		_ = term.Restore(int(os.Stdin.Fd()), state)
	}, nil
}

func copyStringMap(values map[string]string) map[string]string {
	return maps.Clone(values)
}

func envPairs(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := sortedKeys(values)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+values[key])
	}
	return pairs
}

func labelFilters(values map[string]string) filters.Args {
	args := filters.NewArgs()
	for _, key := range sortedKeys(values) {
		args.Add("label", key+"="+values[key])
	}
	return args
}

func toAPIMounts(mounts []Mount) []mountapi.Mount {
	if len(mounts) == 0 {
		return nil
	}

	apiMounts := make([]mountapi.Mount, 0, len(mounts))
	for _, mount := range mounts {
		apiMounts = append(apiMounts, mountapi.Mount{
			Type:     mountapi.TypeBind,
			Source:   mount.Source,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}
	return apiMounts
}

func sortedKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
