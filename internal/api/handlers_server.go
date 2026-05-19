package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"xdcc-go/internal/store"
)

// =========================================================================
// GET /api/servers
// =========================================================================

func (a *API) handleListServers(w http.ResponseWriter, r *http.Request) {
	// If IRC manager is available, use it for live status overlay
	if a.IRCManager != nil {
		servers := a.IRCManager.GetServers()
		writeJSON(w, http.StatusOK, servers)
		return
	}

	// Fallback: get from store directly
	servers, err := a.Store.ListServers()
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "LIST_SERVERS_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

// =========================================================================
// POST /api/servers
// =========================================================================

func (a *API) handleConnectServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address     string `json:"address"`
		Port        int    `json:"port"`
		AutoConnect bool   `json:"auto_connect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if body.Address == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ADDRESS", "server address is required")
		return
	}
	if body.Port < 1 || body.Port > 65535 {
		body.Port = 6667
	}

	// Add to store
	id, err := a.Store.AddServer(store.ServerRecord{
		Address:     body.Address,
		Port:        body.Port,
		AutoConnect: body.AutoConnect,
		Status:      "disconnected",
	})
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "ADD_SERVER_ERROR", err.Error())
		return
	}

	// Connect via IRC manager if available
	if a.IRCManager != nil {
		if err := a.IRCManager.ConnectServerByID(id); err != nil {
			a.Logger.Printf("WARNING: connecting to server %s failed: %v", body.Address, err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// =========================================================================
// DELETE /api/servers/:serverID
// =========================================================================

func (a *API) handleDisconnectServer(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid server ID")
		return
	}

	if a.IRCManager != nil {
		if err := a.IRCManager.DisconnectServer(id); err != nil {
			a.logAndError(w, http.StatusInternalServerError, "DISCONNECT_ERROR", err.Error())
			return
		}
	}

	if err := a.Store.DeleteServer(id); err != nil {
		a.logAndError(w, http.StatusInternalServerError, "DELETE_SERVER_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =========================================================================
// GET /api/servers/:serverID/channels
// =========================================================================

func (a *API) handleListChannels(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid server ID")
		return
	}

	// If IRC manager is available, use it for live status overlay
	if a.IRCManager != nil {
		channels := a.IRCManager.GetChannels(id)
		writeJSON(w, http.StatusOK, channels)
		return
	}

	// Fallback: get from store directly
	channels, err := a.Store.GetChannelsByServer(id)
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "LIST_CHANNELS_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

// =========================================================================
// POST /api/servers/:serverID/channels
// =========================================================================

func (a *API) handleJoinChannel(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseID(chi.URLParam(r, "serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid server ID")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CHANNEL", "channel name is required")
		return
	}

	chID, err := a.Store.AddChannel(store.ChannelRecord{
		ServerID: serverID,
		Name:     body.Name,
		AutoJoin: true,
		Joined:   false,
	})
	if err != nil {
		a.logAndError(w, http.StatusInternalServerError, "ADD_CHANNEL_ERROR", err.Error())
		return
	}

	if a.IRCManager != nil {
		if err := a.IRCManager.JoinChannel(serverID, body.Name); err != nil {
			a.Logger.Printf("WARNING: joining channel %s failed: %v", body.Name, err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]int64{"id": chID})
}

// =========================================================================
// DELETE /api/servers/:serverID/channels/:channelName
// =========================================================================

func (a *API) handleLeaveChannel(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseID(chi.URLParam(r, "serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid server ID")
		return
	}
	channelName := chi.URLParam(r, "channelName")

	if a.IRCManager != nil {
		if err := a.IRCManager.LeaveChannel(serverID, channelName); err != nil {
			a.Logger.Printf("WARNING: leaving channel %s failed: %v", channelName, err)
		}
	}

	// Remove from store
	if ch, err := a.Store.GetChannelsByServerAndName(serverID, channelName); err == nil && ch != nil {
		_ = a.Store.DeleteChannel(ch.ID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// =========================================================================
// GET /api/servers/:serverID/channels/:channelName/topic
// =========================================================================

func (a *API) handleGetChannelTopic(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseID(chi.URLParam(r, "serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid server ID")
		return
	}
	channelName := chi.URLParam(r, "channelName")

	if a.IRCManager == nil {
		writeError(w, http.StatusServiceUnavailable, "IRC_UNAVAILABLE", "IRC manager not available")
		return
	}

	topic, err := a.IRCManager.GetChannelTopic(serverID, channelName)
	if err != nil {
		writeError(w, http.StatusNotFound, "CHANNEL_NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"topic": topic})
}
