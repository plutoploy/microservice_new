package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/moby/moby/client"
)

type ContainerManager interface {
	ContainerCreate(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ImagePull(ctx context.Context, image string, opts client.ImagePullOptions) (client.ImagePullResponse, error)
	ContainerStart(ctx context.Context, id string, opts client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerList(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerStop(ctx context.Context, id string, opts client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRemove(ctx context.Context, id string, opts client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	ContainerRestart(ctx context.Context, id string, opts client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	ContainerInspect(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerRename(ctx context.Context, id string, opts client.ContainerRenameOptions) (client.ContainerRenameResult, error)
}

type DockerManager struct {
	client *client.Client
}

func NewContainerManager() (ContainerManager, error) {
	cli, err := getClient()
	if err != nil {
		return nil, err
	}
	return &DockerManager{client: cli}, nil
}

// candidateSocketPaths returns the ordered list of Docker/Podman socket paths
// to fall back to ONLY when $DOCKER_HOST is not set. $DOCKER_HOST always takes
// precedence and is never overridden by these.
func candidateSocketPaths() []string {
	rootlessPaths := make([]string, 0, 5)
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		rootlessPaths = append(rootlessPaths,
			filepath.Join(runtimeDir, "podman/podman.sock"),
			filepath.Join(runtimeDir, "docker.sock"),
		)
	}
	if uid := os.Getuid(); uid >= 0 {
		rootlessPaths = append(rootlessPaths,
			fmt.Sprintf("/run/user/%d/podman/podman.sock", uid),
			fmt.Sprintf("/run/user/%d/docker.sock", uid),
		)
	}
	return append(rootlessPaths, "/var/run/docker.sock")
}

// Precedence:
//  1. $DOCKER_HOST (authoritative; used verbatim, no socket probing)
//  2. the first existing socket from candidateSocketPaths() (fallback only)

func resolveDockerHost() (string, bool) {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host, true
	}
	for _, path := range candidateSocketPaths() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return "unix://" + path, true
		}
	}
	return "", false
}

func ensureDockerHost() error {
	if os.Getenv("DOCKER_HOST") != "" {
		return nil
	}
	if host, ok := resolveDockerHost(); ok {
		return os.Setenv("DOCKER_HOST", host)
	}
	return nil
}

func getClient() (*client.Client, error) {
	// $DOCKER_HOST is authoritative: defer entirely to client.FromEnv so the
	// agent targets exactly what the environment specifies (no socket probing).
	if os.Getenv("DOCKER_HOST") != "" {
		return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	}

	// Otherwise fall back to a discovered socket.
	if host, ok := resolveDockerHost(); ok {
		return client.NewClientWithOpts(
			client.WithHost(host),
			client.WithAPIVersionNegotiation(),
		)
	}
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

func (m *DockerManager) ContainerCreate(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	return m.client.ContainerCreate(ctx, opts)
}

func (m *DockerManager) ImagePull(ctx context.Context, image string, opts client.ImagePullOptions) (client.ImagePullResponse, error) {
	return m.client.ImagePull(ctx, image, opts)
}

func (m *DockerManager) ContainerStart(ctx context.Context, id string, opts client.ContainerStartOptions) (client.ContainerStartResult, error) {
	return m.client.ContainerStart(ctx, id, opts)
}

func (m *DockerManager) ContainerList(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	return m.client.ContainerList(ctx, opts)
}

func (m *DockerManager) ContainerStop(ctx context.Context, id string, opts client.ContainerStopOptions) (client.ContainerStopResult, error) {
	return m.client.ContainerStop(ctx, id, opts)
}

func (m *DockerManager) ContainerRemove(ctx context.Context, id string, opts client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	return m.client.ContainerRemove(ctx, id, opts)
}

func (m *DockerManager) ContainerRestart(ctx context.Context, id string, opts client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	return m.client.ContainerRestart(ctx, id, opts)
}

func (m *DockerManager) ContainerInspect(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return m.client.ContainerInspect(ctx, id, opts)
}

func (m *DockerManager) ContainerRename(ctx context.Context, id string, opts client.ContainerRenameOptions) (client.ContainerRenameResult, error) {
	return m.client.ContainerRename(ctx, id, opts)
}
