package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"xdcc-go/internal/sse"
)

// =========================================================================
// GET /api/events — SSE stream for real-time updates (Fase 7.1)
// =========================================================================

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Safely get request ID from context
	reqID := "unknown"
	if id := r.Context().Value("request-id"); id != nil {
		if idStr, ok := id.(string); ok {
			reqID = idStr
		}
	}
	
	start := time.Now()
	
	// Log SSE client connection with current client count
	clientsBefore := a.SSEHub.ClientCount()
	a.Logger.Debugf("[SSE] client connected [%s] remote=%s clients_before=%d", reqID, r.RemoteAddr, clientsBefore)
	defer func() {
		duration := time.Since(start)
		clientsAfter := a.SSEHub.ClientCount()
		a.Logger.Debugf("[SSE] client disconnected [%s] duration=%v clients_after=%d", reqID, duration, clientsAfter)
	}()
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE_UNSUPPORTED",
			"Streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Subscribe to the SSE hub
	ch := a.SSEHub.Subscribe()
	defer a.SSEHub.Unsubscribe(ch)
	
	// Log after subscription
	clientsAfterSub := a.SSEHub.ClientCount()
	a.Logger.Debugf("[SSE] subscribed [%s] total_clients=%d", reqID, clientsAfterSub)

	// Handle Last-Event-ID reconnection (Fase 7.5)
	lastEventIDStr := r.Header.Get("Last-Event-ID")
	var lastEventID int64
	if lastEventIDStr != "" {
		fmt.Sscanf(lastEventIDStr, "%d", &lastEventID)
	}

	// If Last-Event-ID is provided, replay missed events
	if lastEventID > 0 {
		missed := a.SSEHub.EventsSince(lastEventID)
		if missed == nil {
			// Event ID too old — send resync_required
			data, _ := json.Marshal(map[string]string{
				"message": "Event history too old, please reload state via API",
			})
			fmt.Fprintf(w, "event: resync_required\ndata: %s\n\n", data)
			flusher.Flush()
		} else {
			for _, evt := range missed {
				writeSSEEvent(w, evt)
				flusher.Flush()
			}
		}
	}

	// Notify the client of successful connection
	connectedData, _ := json.Marshal(map[string]interface{}{
		"status":    "connected",
		"server_id": a.SSEHub.LastEventID(),
	})
	fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connectedData)
	flusher.Flush()

	// Keepalive ticker (every 30 seconds)
	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	// Main event loop
	a.Logger.Debugf("[SSE] entering main event loop [%s]", reqID)
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected or server shutting down
			a.Logger.Debugf("[SSE] context canceled [%s]: %v", reqID, r.Context().Err())
			return
		case <-keepalive.C:
			// Send keepalive comment to prevent connection timeout
			a.Logger.Debugf("[SSE] sending keepalive [%s]", reqID)
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				// Hub closed - channel was closed
				a.Logger.Debugf("[SSE] channel closed (hub shutdown) [%s]", reqID)
				return
			}
			a.Logger.Debugf("[SSE] received event type=%s [%s]", evt.Type, reqID)
			writeSSEEvent(w, evt)
			flusher.Flush()
		}
	}
}

// writeSSEEvent serializes an sse.Event to SSE format and writes it.
func writeSSEEvent(w http.ResponseWriter, evt sse.Event) {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		return
	}

	// Format: event: <type>\nid: <id>\ndata: <json>\n\n
	fmt.Fprintf(w, "event: %s\n", evt.Type)
	fmt.Fprintf(w, "id: %d\n", evt.ID)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
