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

func getClient() (*client.Client, error) {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	}

	rootlessPaths := make([]string, 0, 4)
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
	rootlessPaths = append(rootlessPaths, "/var/run/docker.sock")

	for _, path := range rootlessPaths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return client.NewClientWithOpts(
				client.WithHost("unix://"+path),
				client.WithAPIVersionNegotiation(),
			)
		}
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
