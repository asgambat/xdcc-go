package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"xdcc-go/internal/searchagg"
	"xdcc-go/internal/store"
)

// =========================================================================
// GET /api/search — aggregated search
// =========================================================================

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	q := r.URL.Query()
	opts := searchagg.SearchOptions{
		Query:    q.Get("q"),
		Prefix:   q.Get("prefix"),
		Bot:      q.Get("bot"),
		Compact:  q.Get("compact") == "true",
		Page:     1,
		PageSize: 50,
	}
	if ext := q.Get("ext"); ext != "" {
		opts.Ext = strings.Split(ext, ",")
	}
	if p := q.Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &opts.Page)
	}
	if ps := q.Get("pageSize"); ps != "" {
		fmt.Sscanf(ps, "%d", &opts.PageSize)
	}
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 || opts.PageSize > 500 {
		opts.PageSize = 50
	}

	// Create context with 30s timeout
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := a.SearchAggregator.Search(ctx, opts)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "SEARCH_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =========================================================================
// GET /api/search/presets
// =========================================================================

func (a *API) handleListPresets(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	presets, err := a.SearchAggregator.ListPresets()
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "LIST_PRESETS_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, presets)
}

// =========================================================================
// POST /api/search/presets
// =========================================================================

func (a *API) handleCreatePreset(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	var body struct {
		Name      string `json:"name"`
		Query     string `json:"query"`
		Filters   string `json:"filters_json"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "name is required")
		return
	}
	if body.Query == "" {
		writeError(w, http.StatusBadRequest, "MISSING_QUERY", "query is required")
		return
	}

	id, err := a.SearchAggregator.CreatePreset(body.Name, body.Query, body.Filters, body.IsDefault)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "CREATE_PRESET_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// =========================================================================
// PUT /api/search/presets/:presetID
// =========================================================================

func (a *API) handleUpdatePreset(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	id, err := parseID(chi.URLParam(r, "presetID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid preset ID")
		return
	}

	// Get existing preset
	existing, err := a.SearchAggregator.GetPreset(id)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "GET_PRESET_ERROR", err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "PRESET_NOT_FOUND", fmt.Sprintf("Preset %d not found", id))
		return
	}

	var body struct {
		Name      string `json:"name"`
		Query     string `json:"query"`
		Filters   string `json:"filters_json"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	// Build updated preset
	updated := *existing
	if body.Name != "" {
		updated.Name = body.Name
	}
	if body.Query != "" {
		updated.Query = body.Query
	}
	updated.FiltersJSON = body.Filters
	updated.IsDefault = body.IsDefault

	if err := a.SearchAggregator.UpdatePreset(updated); err != nil {
		a.logAndError(w, http.StatusInternalServerError, "UPDATE_PRESET_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// =========================================================================
// DELETE /api/search/presets/:presetID
// =========================================================================

func (a *API) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	id, err := parseID(chi.URLParam(r, "presetID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid preset ID")
		return
	}

	if err := a.SearchAggregator.DeletePreset(id); err != nil {
		a.logAndError(w, http.StatusInternalServerError, "DELETE_PRESET_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =========================================================================
// GET /api/watchlists
// =========================================================================

func (a *API) handleListWatchlists(w http.ResponseWriter, r *http.Request) {
	watchlists, err := a.Store.ListWatchlists()
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "LIST_WATCHLISTS_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, watchlists)
}

// =========================================================================
// POST /api/watchlists
// =========================================================================

func (a *API) handleCreateWatchlist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Query       string `json:"query"`
		FiltersJSON string `json:"filters_json"`
		Enabled     bool   `json:"enabled"`
		AutoEnqueue bool   `json:"auto_enqueue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "name is required")
		return
	}
	if body.Query == "" {
		writeError(w, http.StatusBadRequest, "MISSING_QUERY", "query is required")
		return
	}

	id, err := a.Store.AddWatchlist(store.Watchlist{
		Name:        body.Name,
		Query:       body.Query,
		FiltersJSON: body.FiltersJSON,
		Enabled:     body.Enabled,
		AutoEnqueue: body.AutoEnqueue,
	})
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "CREATE_WATCHLIST_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// =========================================================================
// PUT /api/watchlists/:watchlistID
// =========================================================================

func (a *API) handleUpdateWatchlist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "watchlistID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid watchlist ID")
		return
	}

	// Get existing
	existing, err := a.Store.GetWatchlist(id)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "GET_WATCHLIST_ERROR", err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "WATCHLIST_NOT_FOUND", fmt.Sprintf("Watchlist %d not found", id))
		return
	}

	var body struct {
		Name        string `json:"name"`
		Query       string `json:"query"`
		FiltersJSON string `json:"filters_json"`
		Enabled     bool   `json:"enabled"`
		AutoEnqueue bool   `json:"auto_enqueue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	updated := *existing
	if body.Name != "" {
		updated.Name = body.Name
	}
	if body.Query != "" {
		updated.Query = body.Query
	}
	updated.FiltersJSON = body.FiltersJSON
	updated.Enabled = body.Enabled
	updated.AutoEnqueue = body.AutoEnqueue

	if err := a.Store.UpdateWatchlist(updated); err != nil {
		a.logAndError(w, http.StatusInternalServerError, "UPDATE_WATCHLIST_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// =========================================================================
// DELETE /api/watchlists/:watchlistID
// =========================================================================

func (a *API) handleDeleteWatchlist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "watchlistID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid watchlist ID")
		return
	}

	if err := a.Store.DeleteWatchlist(id); err != nil {
		a.logAndError(w, http.StatusInternalServerError, "DELETE_WATCHLIST_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =========================================================================
// POST /api/watchlists/:watchlistID/run
// =========================================================================

func (a *API) handleRunWatchlist(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	id, err := parseID(chi.URLParam(r, "watchlistID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid watchlist ID")
		return
	}

	// Get the watchlist
	wl, err := a.Store.GetWatchlist(id)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "GET_WATCHLIST_ERROR", err.Error())
		return
	}
	if wl == nil {
		writeError(w, http.StatusNotFound, "WATCHLIST_NOT_FOUND", fmt.Sprintf("Watchlist %d not found", id))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	result, err := a.SearchAggregator.RunWatchlist(ctx, *wl)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "RUN_WATCHLIST_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =========================================================================
// GET /api/search/providers — provider status and insights
// =========================================================================

func (a *API) handleGetProviders(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	// Return both states and insights
	states := a.SearchAggregator.GetProviderStates()
	insights, err := a.SearchAggregator.GetProviderInsights()
	if err != nil {
		// Non-fatal — just log
		a.Logger.Printf("WARNING: getting provider insights: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"states":   states,
		"insights": insights,
	})
}

// =========================================================================
// PATCH /api/search/providers/:providerName — enable/disable provider
// =========================================================================

func (a *API) handlePatchProvider(w http.ResponseWriter, r *http.Request) {
	if a.SearchAggregator == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "Search aggregator not available")
		return
	}

	name := chi.URLParam(r, "providerName")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	if body.Enabled {
		a.SearchAggregator.EnableProvider(name)
	} else {
		a.SearchAggregator.DisableProvider(name)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": name,
		"enabled":  body.Enabled,
	})
}

// =========================================================================
// POST /api/xdcc/parse — parse raw XDCC command
// =========================================================================

func (a *API) handleParseXDCC(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	cmd := strings.TrimSpace(body.Command)
	if cmd == "" {
		writeError(w, http.StatusBadRequest, "MISSING_COMMAND", "command is required")
		return
	}

	// Parse common XDCC command formats:
	// /msg Bot XDCC SEND #123
	// /msg Bot xdcc send #123
	// Bot: xdcc send #123
	// xdcc send #123 (implicit bot)

	bot := ""
	packNum := 0

	// Format: /msg <bot> XDCC SEND #<num>
	if strings.HasPrefix(cmd, "/msg ") {
		parts := strings.Fields(cmd)
		if len(parts) >= 4 && strings.EqualFold(parts[2], "XDCC") && strings.EqualFold(parts[3], "SEND") && len(parts) >= 5 {
			bot = parts[1]
			fmt.Sscanf(parts[4], "#%d", &packNum)
		}
	}

	// Format: <bot>: xdcc send #<num>
	if bot == "" && packNum == 0 {
		if idx := strings.Index(cmd, ":"); idx > 0 {
			rest := strings.TrimSpace(cmd[idx+1:])
			if strings.Contains(strings.ToLower(rest), "xdcc send") {
				bot = strings.TrimSpace(cmd[:idx])
				parts := strings.Fields(rest)
				for i, p := range parts {
					if strings.EqualFold(p, "send") && i+1 < len(parts) {
						fmt.Sscanf(parts[i+1], "#%d", &packNum)
						break
					}
				}
			}
		}
	}

	// Format: xdcc send #<num> (bot unknown)
	if bot == "" && packNum == 0 {
		parts := strings.Fields(cmd)
		for i, p := range parts {
			if strings.EqualFold(p, "send") && i+1 < len(parts) {
				fmt.Sscanf(parts[i+1], "#%d", &packNum)
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"parsed":       packNum > 0,
		"bot":          bot,
		"pack_number":  packNum,
		"pack_message": fmt.Sprintf("xdcc send #%d", packNum),
		"original":     cmd,
	})
}
