package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"xdcc-go/internal/config"
	"xdcc-go/internal/store"
)

// =========================================================================
// GET /healthz
// =========================================================================

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =========================================================================
// GET /readyz
// =========================================================================

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	_, err := a.Store.CurrentSchemaVersion()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"error":  "database not available",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// =========================================================================
// GET /api/version
// =========================================================================

func (a *API) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":                       "0.2.0",
		"min_compatible_client_version": "0.2.0",
	})
}

// =========================================================================
// GET /api/config
// =========================================================================

func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Config)
}

// =========================================================================
// PUT /api/config
// =========================================================================

func (a *API) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	cfg := a.Config // already *config.Config

	if v, ok := body["download"].(map[string]interface{}); ok {
		if mp, ok := v["max_parallel_total"].(float64); ok {
			cfg.Download.MaxParallelTotal = int(mp)
		}
		if cp, ok := v["conflict_policy"].(string); ok {
			cfg.Download.ConflictPolicy = cp
		}
		if ff, ok := v["fail_fallback"].(string); ok {
			cfg.Download.FailFallback = ff
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// =========================================================================
// GET /api/stats
// =========================================================================

func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	queue, _ := a.Store.GetQueue()

	queueCount := 0
	activeCount := 0
	totalSpeedBPS := int64(0)
	for _, item := range queue {
		switch item.Status {
		case "queued":
			queueCount++
		case "downloading":
			activeCount++
			totalSpeedBPS += item.SpeedBPS
		}
	}

	totalDownloadedBytes, _ := a.Store.GetTotalDownloadedBytes()

	_, totalHistory, _ := a.Store.GetDownloadHistory(1, 1, store.HistoryFilter{})

	servers, _ := a.Store.ListServers()
	serverCount := len(servers)

	uptimeSeconds := int64(time.Since(a.StartTime).Seconds())

	// Get disk info
	di, err := getDiskInfo(a.Config.Download.DestDir)
	diskFreeBytes := int64(0)
	diskTotalBytes := int64(0)
	if err == nil {
		diskFreeBytes = di.available
		diskTotalBytes = di.total
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_downloads":       activeCount,
		"queued_downloads":       queueCount,
		"total_completed":        totalHistory,
		"connected_servers":      serverCount,
		"total_downloaded_bytes": totalDownloadedBytes,
		"average_speed_bps":      totalSpeedBPS,
		"uptime_seconds":         uptimeSeconds,
		"disk_free_bytes":        diskFreeBytes,
		"disk_total_bytes":       diskTotalBytes,
		"started_at":             a.StartTime.Format(time.RFC3339),
		"go_version":             runtime.Version(),
		"os":                     runtime.GOOS + "/" + runtime.GOARCH,
	})
}

// =========================================================================
// GET /api/status
// =========================================================================

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	warnings := make([]string, 0)
	info := make(map[string]interface{})

	di, err := getDiskInfo(a.Config.Download.DestDir)
	diskFreeBytes := int64(0)
	diskTotalBytes := int64(0)
	if err == nil {
		diskFreeBytes = di.available
		diskTotalBytes = di.total
		if di.available < 1*1024*1024*1024 {
			warnings = append(warnings, "Low disk space in download directory")
		}
	}

	servers, _ := a.Store.ListServers()
	connectedServers := 0
	totalServers := len(servers)
	for _, srv := range servers {
		if srv.Status == "connected" {
			connectedServers++
		}
	}
	info["servers"] = map[string]int{"connected": connectedServers, "total": totalServers}

	queue, _ := a.Store.GetQueue()
	activeDownloads := 0
	for _, item := range queue {
		if item.Status == "downloading" {
			activeDownloads++
		}
	}
	info["active_downloads"] = activeDownloads

	uptimeSeconds := int64(time.Since(a.StartTime).Seconds())

	status := "healthy"
	if len(warnings) > 0 {
		status = "degraded"
	}
	if totalServers > 0 && connectedServers == 0 {
		warnings = append(warnings, "No IRC servers connected")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":           status,
		"warnings":         warnings,
		"info":             info,
		"uptime_seconds":   uptimeSeconds,
		"disk_free_bytes":  diskFreeBytes,
		"disk_total_bytes": diskTotalBytes,
	})
}

// =========================================================================
// disk helpers
// =========================================================================

type diskInfo struct {
	available int64
	total     int64
	used      int64
}

// =========================================================================
// POST /api/admin/export
// =========================================================================

func (a *API) handleAdminExport(w http.ResponseWriter, r *http.Request) {
	export, err := a.Store.ExportData()
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "EXPORT_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exported_at": time.Now().Format(time.RFC3339),
		"data":        export,
	})
}

// =========================================================================
// GET /api/logs
// =========================================================================

func (a *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	count := 100
	if n := r.URL.Query().Get("count"); n != "" {
		if parsed, err := parseInt(n); err == nil && parsed > 0 {
			count = parsed
		}
	}

	var entries []interface{}
	if a.LogBroadcaster != nil {
		for _, e := range a.LogBroadcaster.RecentEntries(count) {
			entries = append(entries, map[string]interface{}{
				"timestamp": e.Timestamp,
				"level":     e.Level,
				"message":   e.Message,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  entries,
		"count": len(entries),
	})
}

// =========================================================================
// POST /api/admin/import
// =========================================================================

func (a *API) handleAdminImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data *store.ExportData `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	if body.Data == nil {
		writeError(w, http.StatusBadRequest, "MISSING_DATA", "data is required")
		return
	}

	if err := a.Store.ImportData(body.Data); err != nil {
		a.logAndError(w, http.StatusInternalServerError, "IMPORT_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "imported"})
}

// =========================================================================
// GET /api/setup/status
// =========================================================================

func (a *API) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	completed := a.Config.UI.SetupCompleted

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_completed": completed,
	})
}

// =========================================================================
// POST /api/setup/bootstrap
// =========================================================================

func (a *API) handleSetupBootstrap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname      string `json:"nickname"`
		ServerAddress string `json:"server_address"`
		ServerPort    int    `json:"server_port"`
		DownloadDir   string `json:"download_dir"`
		TempDir       string `json:"temp_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	cfg := a.Config

	if body.Nickname != "" {
		cfg.IRC.Nickname = body.Nickname
	}
	if body.ServerAddress != "" {
		port := body.ServerPort
		if port < 1 || port > 65535 {
			port = 6667
		}
		cfg.IRC.DefaultServers = []config.ServerConfig{
			{
				Address:     body.ServerAddress,
				Port:        port,
				AutoConnect: true,
			},
		}
	}
	if body.DownloadDir != "" {
		cfg.Download.DestDir = body.DownloadDir
	}
	if body.TempDir != "" {
		cfg.Download.TempDir = body.TempDir
	}

	cfg.UI.SetupCompleted = true

	for _, dir := range []string{cfg.Download.TempDir, cfg.Download.DestDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			a.logAndError(w, http.StatusInternalServerError, "MKDIR_ERROR",
				fmt.Sprintf("creating directory %s: %v", dir, err))
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "setup_completed"})
}
