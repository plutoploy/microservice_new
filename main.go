package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var dockerClient *client.Client

func main() {
	var err error
	dockerClient, err = client.New(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create docker client: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /containers", handleContainerCreate)
	mux.HandleFunc("GET /containers", handleContainerList)
	mux.HandleFunc("DELETE /containers/{id}", handleContainerTeardown)
	mux.HandleFunc("PUT /containers/{id}", handleContainerReplace)
	mux.HandleFunc("POST /containers/{id}/start", handleContainerStart)
	mux.HandleFunc("POST /containers/{id}/stop", handleContainerStop)
	mux.HandleFunc("POST /containers/{id}/restart", handleContainerRestart)
	mux.HandleFunc("GET /containers/{id}", handleContainerInspect)
	mux.HandleFunc("POST /containers/{id}/rename", handleContainerRename)
	mux.HandleFunc("GET /containers/{id}/labels", handleContainerGetLabels)
	mux.HandleFunc("PUT /containers/{id}/labels", handleContainerSetLabels)

	log.Println("Container agent listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

type CreateRequest struct {
	Name       string                `json:"name,omitempty"`
	Image      string                `json:"image"`
	Labels     map[string]string     `json:"labels,omitempty"`
	Config     *container.Config     `json:"config,omitempty"`
	HostConfig *container.HostConfig `json:"host_config,omitempty"`
}

type ReplaceRequest struct {
	CreateRequest
}

type RenameRequest struct {
	NewName string `json:"new_name"`
}

type APIResponse struct {
	OK      bool        `json:"ok"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, APIResponse{OK: false, Message: msg})
}

func handleContainerCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Image == "" {
		writeError(w, http.StatusBadRequest, "image is required")
		return
	}

	cfg := req.Config
	if cfg == nil {
		cfg = &container.Config{}
	}

	if len(req.Labels) > 0 {
		if cfg.Labels == nil {
			cfg.Labels = make(map[string]string)
		}
		for k, v := range req.Labels {
			cfg.Labels[k] = v
		}
	}

	opts := client.ContainerCreateOptions{
		Image:      req.Image,
		Config:     cfg,
		HostConfig: req.HostConfig,
		Name:       req.Name,
	}

	result, err := dockerClient.ContainerCreate(context.Background(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, APIResponse{
		OK:      true,
		Message: "container created",
		Data:    map[string]interface{}{"id": result.ID, "warnings": result.Warnings},
	})
}

func handleContainerList(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"

	result, err := dockerClient.ContainerList(context.Background(), client.ContainerListOptions{All: all})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		OK:   true,
		Data: result.Items,
	})
}

func handleContainerTeardown(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	timeout := 10
	dockerClient.ContainerStop(context.Background(), id, client.ContainerStopOptions{Timeout: &timeout})

	_, err := dockerClient.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{Force: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "remove failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "container torn down"})
}

func handleContainerReplace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	var req ReplaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Image == "" {
		writeError(w, http.StatusBadRequest, "image is required")
		return
	}

	timeout := 10
	dockerClient.ContainerStop(context.Background(), id, client.ContainerStopOptions{Timeout: &timeout})
	dockerClient.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{Force: true})

	cfg := req.Config
	if cfg == nil {
		cfg = &container.Config{}
	}

	if len(req.Labels) > 0 {
		if cfg.Labels == nil {
			cfg.Labels = make(map[string]string)
		}
		for k, v := range req.Labels {
			cfg.Labels[k] = v
		}
	}

	opts := client.ContainerCreateOptions{
		Image:      req.Image,
		Config:     cfg,
		HostConfig: req.HostConfig,
		Name:       req.Name,
	}

	result, err := dockerClient.ContainerCreate(context.Background(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "replace create failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, APIResponse{
		OK:      true,
		Message: "container replaced",
		Data:    map[string]interface{}{"id": result.ID, "warnings": result.Warnings},
	})
}

func handleContainerStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	_, err := dockerClient.ContainerStart(context.Background(), id, client.ContainerStartOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "container started"})
}

func handleContainerStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	timeout := 10
	_, err := dockerClient.ContainerStop(context.Background(), id, client.ContainerStopOptions{Timeout: &timeout})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stop failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "container stopped"})
}

func handleContainerRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	timeout := 10
	_, err := dockerClient.ContainerRestart(context.Background(), id, client.ContainerRestartOptions{Timeout: &timeout})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restart failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "container restarted"})
}

func handleContainerInspect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	result, err := dockerClient.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inspect failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		OK:   true,
		Data: result.Container,
	})
}

func handleContainerRename(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	var req RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.NewName == "" {
		writeError(w, http.StatusBadRequest, "new_name is required")
		return
	}

	_, err := dockerClient.ContainerRename(context.Background(), id, client.ContainerRenameOptions{NewName: req.NewName})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rename failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Message: "container renamed"})
}

func handleContainerGetLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	result, err := dockerClient.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inspect failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		OK:   true,
		Data: result.Container.Config.Labels,
	})
}

type LabelsRequest struct {
	Labels map[string]string `json:"labels"`
}

func handleContainerSetLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	var req LabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	result, err := dockerClient.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inspect failed: "+err.Error())
		return
	}

	existing := result.Container.Config.Labels
	if existing == nil {
		existing = make(map[string]string)
	}
	for k, v := range req.Labels {
		existing[k] = v
	}

	result.Container.Config.Labels = existing

	writeJSON(w, http.StatusOK, APIResponse{
		OK:      true,
		Message: "labels updated (restart required to apply)",
		Data:    existing,
	})
}
