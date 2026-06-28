package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	wtcontainer "github.com/dockerutil/watchtower/pkg/container"
	"github.com/dockerutil/watchtower/pkg/filters"
	"github.com/dockerutil/watchtower/pkg/session"
	"github.com/dockerutil/watchtower/pkg/sorter"
	wttypes "github.com/dockerutil/watchtower/pkg/types"
)

// Updater wraps the watchtower container client so the rest of the agent does
// not need to know about watchtower internals. It checks running containers for
// updated images and (optionally) recreates them with the newer image.
//
// The watchtower update algorithm proper lives in an internal package and
// cannot be imported, so the update loop here is implemented on top of the
// exported pkg/container.Client interface. It mirrors the core behaviour:
// list -> detect stale image -> stop -> recreate from stored config -> start,
// with optional old-image cleanup.
type Updater interface {
	// Update pulls newer images for the matched containers and recreates them.
	Update(params UpdateRequest) (wttypes.Report, error)
	// Check reports which matched containers have a newer image available
	// without restarting anything (monitor-only).
	Check(params UpdateRequest) (wttypes.Report, error)
}

type watchtowerUpdater struct {
	client wtcontainer.Client
}

// NewUpdater builds a watchtower-backed Updater. It relies on the Docker/Podman
// endpoint resolved by getClient(): we export DOCKER_HOST so watchtower's
// FromEnv client targets the same socket the agent discovered.
func NewUpdater() (Updater, error) {
	if err := ensureDockerHost(); err != nil {
		return nil, err
	}

	client := wtcontainer.NewClient(wtcontainer.ClientOptions{
		IncludeStopped:    false,
		ReviveStopped:     false,
		RemoveVolumes:     false,
		IncludeRestarting: true,
		WarnOnHeadFailed:  wtcontainer.WarnAuto,
	})

	return &watchtowerUpdater{client: client}, nil
}

// UpdateRequest is the JSON body accepted by the update/check endpoints.
type UpdateRequest struct {
	// Names limits the operation to containers with these names. Empty == all.
	Names []string `json:"names,omitempty"`
	// DisableNames excludes containers with these names.
	DisableNames []string `json:"disable_names,omitempty"`
	// EnableLabel only considers containers with the
	// com.centurylinklabs.watchtower.enable=true label
	EnableLabel bool `json:"enable_label,omitempty"`
	// Scope limits the operation to a watchtower scope label value.
	Scope string `json:"scope,omitempty"`
	// Cleanup removes the old image after a successful update.
	Cleanup bool `json:"cleanup,omitempty"`
	// NoRestart updates the image but does not recreate the container.
	NoRestart bool `json:"no_restart,omitempty"`
	// NoPull skips pulling and only acts on already-present images.
	NoPull bool `json:"no_pull,omitempty"`
	// TimeoutSeconds is the per-container stop timeout (default stopTimeout).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// MonitorOnly only scans/reports; never recreates (forced for /check).
	MonitorOnly bool `json:"monitor_only,omitempty"`
}

func (r UpdateRequest) toParams(monitorOnly bool) wttypes.UpdateParams {
	filter, _ := filters.BuildFilter(r.Names, r.DisableNames, r.EnableLabel, r.Scope)

	timeout := time.Duration(stopTimeout) * time.Second
	if r.TimeoutSeconds > 0 {
		timeout = time.Duration(r.TimeoutSeconds) * time.Second
	}

	return wttypes.UpdateParams{
		Filter:      filter,
		Cleanup:     r.Cleanup,
		NoRestart:   r.NoRestart,
		NoPull:      r.NoPull,
		Timeout:     timeout,
		MonitorOnly: monitorOnly || r.MonitorOnly,
	}
}

func (u *watchtowerUpdater) Update(params UpdateRequest) (wttypes.Report, error) {
	return u.run(params.toParams(false))
}

func (u *watchtowerUpdater) Check(params UpdateRequest) (wttypes.Report, error) {
	return u.run(params.toParams(true))
}

// run performs the core update session over the public client interface.
func (u *watchtowerUpdater) run(params wttypes.UpdateParams) (wttypes.Report, error) {
	progress := &session.Progress{}

	containers, err := u.client.ListContainers(params.Filter)
	if err != nil {
		return nil, err
	}

	// Detect staleness for every matched container.
	for i := range containers {
		stale, newestImage, err := u.client.IsContainerStale(containers[i], params)
		if err != nil {
			progress.AddSkipped(containers[i], err)
			containers[i].SetStale(false)
			continue
		}
		// Make sure we have enough information to recreate it later.
		if stale && !params.MonitorOnly && !params.NoRestart {
			if verr := containers[i].VerifyConfiguration(); verr != nil {
				progress.AddSkipped(containers[i], verr)
				containers[i].SetStale(false)
				continue
			}
		}
		containers[i].SetStale(stale)
		progress.AddScanned(containers[i], newestImage)
	}

	// Monitor-only or no-restart: report without touching containers.
	if params.MonitorOnly || params.NoRestart {
		return progress.Report(), nil
	}

	u.recreateStale(containers, params, progress)
	return progress.Report(), nil
}

// recreateStale stops and recreates every stale container, oldest first, then
// optionally removes the superseded image.
func (u *watchtowerUpdater) recreateStale(containers []wttypes.Container, params wttypes.UpdateParams, progress *session.Progress) {
	sort.Sort(sorter.ByCreated(containers))

	for _, c := range containers {
		if !c.IsStale() {
			continue
		}

		if err := u.client.StopContainer(c, params.Timeout); err != nil {
			progress.UpdateFailed(map[wttypes.ContainerID]error{c.ID(): err})
			continue
		}

		if _, err := u.client.StartContainer(c); err != nil {
			progress.UpdateFailed(map[wttypes.ContainerID]error{c.ID(): err})
			continue
		}

		if params.Cleanup {
			// Best effort: failing to clean the old image is not fatal.
			_ = u.client.RemoveImageByID(c.ImageID())
		}
	}
}

func handleContainerUpdate(w http.ResponseWriter, r *http.Request) {
	runUpdate(w, r, false)
}

func handleContainerCheck(w http.ResponseWriter, r *http.Request) {
	runUpdate(w, r, true)
}

func runUpdate(w http.ResponseWriter, r *http.Request, checkOnly bool) {
	if updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updater not available")
		return
	}

	var req UpdateRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}

	var (
		report wttypes.Report
		err    error
		action string
	)
	if checkOnly {
		report, err = updater.Check(req)
		action = "checked"
	} else {
		report, err = updater.Update(req)
		action = "updated"
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, action+" failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		OK:      true,
		Message: "containers " + action,
		Data:    summarizeReport(report),
	})
}

type reportEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ImageName string `json:"image_name"`
	CurrentID string `json:"current_image_id"`
	LatestID  string `json:"latest_image_id"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

func summarizeReport(report wttypes.Report) map[string]interface{} {
	if report == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"scanned": mapReports(report.Scanned()),
		"updated": mapReports(report.Updated()),
		"failed":  mapReports(report.Failed()),
		"skipped": mapReports(report.Skipped()),
		"stale":   mapReports(report.Stale()),
		"fresh":   mapReports(report.Fresh()),
	}
}

func mapReports(items []wttypes.ContainerReport) []reportEntry {
	out := make([]reportEntry, 0, len(items))
	for _, c := range items {
		out = append(out, reportEntry{
			ID:        string(c.ID()),
			Name:      c.Name(),
			ImageName: c.ImageName(),
			CurrentID: string(c.CurrentImageID()),
			LatestID:  string(c.LatestImageID()),
			State:     c.State(),
			Error:     c.Error(),
		})
	}
	return out
}
