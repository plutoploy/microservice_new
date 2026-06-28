package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var (
	manager     ContainerManager
	updater     Updater
	serverPort  = "8080"
	stopTimeout = 10
)

func init() {
	if p := os.Getenv("PORT"); p != "" {
		serverPort = p
	}
	if t := os.Getenv("STOP_TIMEOUT"); t != "" {
		if val, err := strconv.Atoi(t); err == nil {
			stopTimeout = val
		}
	}
}

func main() {
	var err error
	manager, err = NewContainerManager()
	if err != nil {
		log.Fatalf("Failed to create container manager: %v", err)
	}

	updater, err = NewUpdater()
	if err != nil {
		log.Printf("Warning: updater unavailable: %v", err)
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
	mux.HandleFunc("POST /containers/update", handleContainerUpdate)
	mux.HandleFunc("POST /containers/check", handleContainerCheck)

	log.Printf("Container agent listening on :%s\n", serverPort)
	log.Fatal(http.ListenAndServe(":"+serverPort, mux))
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

func handleContainerError(w http.ResponseWriter, err error, msg string) {
	if errdefs.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, msg+": "+err.Error())
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

	result, err := manager.ContainerCreate(r.Context(), opts)
	if errdefs.IsNotFound(err) {
		// Try pulling the image automatically
		pullResp, pullErr := manager.ImagePull(r.Context(), req.Image, client.ImagePullOptions{})
		if pullErr != nil {
			writeError(w, http.StatusInternalServerError, "image pull failed: "+pullErr.Error())
			return
		}
		if waitErr := pullResp.Wait(r.Context()); waitErr != nil {
			writeError(w, http.StatusInternalServerError, "image pull wait failed: "+waitErr.Error())
			return
		}
		// Retry create after pulling
		result, err = manager.ContainerCreate(r.Context(), opts)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed: "+err.Error())
		return
	}

	if _, err := manager.ContainerStart(r.Context(), result.ID, client.ContainerStartOptions{}); err != nil {
		writeError(w, http.StatusInternalServerError, "start failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, APIResponse{
		OK:      true,
		Message: "container created and started",
		Data:    map[string]interface{}{"id": result.ID, "warnings": result.Warnings},
	})
}

func handleContainerList(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"

	result, err := manager.ContainerList(r.Context(), client.ContainerListOptions{All: all})
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

	timeout := stopTimeout
	if _, err := manager.ContainerStop(r.Context(), id, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		handleContainerError(w, err, "stop failed")
		return
	}

	if _, err := manager.ContainerRemove(r.Context(), id, client.ContainerRemoveOptions{Force: true}); err != nil {
		handleContainerError(w, err, "remove failed")
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

	timeout := stopTimeout
	if _, err := manager.ContainerStop(r.Context(), id, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		writeError(w, http.StatusInternalServerError, "replace stop failed: "+err.Error())
		return
	}
	if _, err := manager.ContainerRemove(r.Context(), id, client.ContainerRemoveOptions{Force: true}); err != nil {
		writeError(w, http.StatusInternalServerError, "replace remove failed: "+err.Error())
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

	result, err := manager.ContainerCreate(r.Context(), opts)
	if errdefs.IsNotFound(err) {
		pullResp, pullErr := manager.ImagePull(r.Context(), req.Image, client.ImagePullOptions{})
		if pullErr != nil {
			writeError(w, http.StatusInternalServerError, "replace image pull failed: "+pullErr.Error())
			return
		}
		if waitErr := pullResp.Wait(r.Context()); waitErr != nil {
			writeError(w, http.StatusInternalServerError, "replace image pull wait failed: "+waitErr.Error())
			return
		}
		result, err = manager.ContainerCreate(r.Context(), opts)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "replace create failed: "+err.Error())
		return
	}

	if _, err := manager.ContainerStart(r.Context(), result.ID, client.ContainerStartOptions{}); err != nil {
		writeError(w, http.StatusInternalServerError, "start failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, APIResponse{
		OK:      true,
		Message: "container replaced and started",
		Data:    map[string]interface{}{"id": result.ID, "warnings": result.Warnings},
	})
}

func handleContainerStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}

	if _, err := manager.ContainerStart(r.Context(), id, client.ContainerStartOptions{}); err != nil {
		handleContainerError(w, err, "start failed")
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

	timeout := stopTimeout
	if _, err := manager.ContainerStop(r.Context(), id, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		handleContainerError(w, err, "stop failed")
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

	timeout := stopTimeout
	if _, err := manager.ContainerRestart(r.Context(), id, client.ContainerRestartOptions{Timeout: &timeout}); err != nil {
		handleContainerError(w, err, "restart failed")
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

	result, err := manager.ContainerInspect(r.Context(), id, client.ContainerInspectOptions{})
	if err != nil {
		handleContainerError(w, err, "inspect failed")
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

	if _, err := manager.ContainerRename(r.Context(), id, client.ContainerRenameOptions{NewName: req.NewName}); err != nil {
		handleContainerError(w, err, "rename failed")
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

	result, err := manager.ContainerInspect(r.Context(), id, client.ContainerInspectOptions{})
	if err != nil {
		handleContainerError(w, err, "inspect failed")
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

	result, err := manager.ContainerInspect(r.Context(), id, client.ContainerInspectOptions{})
	if err != nil {
		handleContainerError(w, err, "inspect failed")
		return
	}

	cfg := result.Container.Config
	if cfg == nil {
		cfg = &container.Config{}
	}

	existing := cfg.Labels
	if existing == nil {
		existing = make(map[string]string)
	}
	for k, v := range req.Labels {
		existing[k] = v
	}
	cfg.Labels = existing

	timeout := stopTimeout
	if _, err := manager.ContainerStop(r.Context(), id, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		writeError(w, http.StatusInternalServerError, "label update stop failed: "+err.Error())
		return
	}
	if _, err := manager.ContainerRemove(r.Context(), id, client.ContainerRemoveOptions{Force: true}); err != nil {
		writeError(w, http.StatusInternalServerError, "label update remove failed: "+err.Error())
		return
	}

	name := result.Container.Name
	name = strings.TrimPrefix(name, "/")

	opts := client.ContainerCreateOptions{
		Image:      cfg.Image,
		Config:     cfg,
		HostConfig: result.Container.HostConfig,
		Name:       name,
	}

	createResult, err := manager.ContainerCreate(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "recreate failed: "+err.Error())
		return
	}

	if _, err := manager.ContainerStart(r.Context(), createResult.ID, client.ContainerStartOptions{}); err != nil {
		writeError(w, http.StatusInternalServerError, "start failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		OK:      true,
		Message: "labels updated and container recreated",
		Data:    cfg.Labels,
	})
}
