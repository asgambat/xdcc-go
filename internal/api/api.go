// Package api implements the REST API + SSE + frontend serving for the
// xdcc-server (Fase 6-8).
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lrstanley/girc"
	"xdcc-go/internal/config"
	"xdcc-go/internal/ircmanager"
	"xdcc-go/internal/logging"
	"xdcc-go/internal/queue"
	"xdcc-go/internal/searchagg"
	"xdcc-go/internal/sse"
	"xdcc-go/internal/store"
)

// =========================================================================
// API — dependency container
// =========================================================================

// API holds all dependencies needed by the HTTP handlers.
type API struct {
	Store            *store.SQLiteStore
	IRCManager       IRCManager
	QueueManager     QueueManager
	SearchAggregator *searchagg.Aggregator
	SSEHub           *sse.Hub
	LogBroadcaster   *logging.LogBroadcaster
	Config           *config.Config
	Logger           *logging.Logger
	StartTime        time.Time
}

// IRCManager defines the subset of ircmanager.Manager methods used by handlers.
type IRCManager interface {
	GetServers() []store.ServerRecord
	GetClient(serverID int64) *girc.Client
	ConnectServerByID(id int64) error
	DisconnectServer(id int64) error
	JoinChannel(serverID int64, channel string) error
	LeaveChannel(serverID int64, channel string) error
	GetChannels(serverID int64) []store.ChannelRecord
	GetChannelTopic(serverID int64, channel string) (string, error)
	// Subscribe returns a channel that receives IRC state change events.
	Subscribe() chan ircmanager.Event
	// Unsubscribe removes a previously subscribed channel.
	Unsubscribe(ch chan ircmanager.Event)
}

// QueueManager defines the subset of queue.QueueManager methods used by handlers.
type QueueManager interface {
	Enqueue(d store.DownloadRecord) (int64, error)
	CancelDownload(id int64, reason string) error
	PauseDownload(id int64) error
	ResumeDownload(id int64) error
	RemoveDownload(id int64) error
	BulkAction(ids []int64, action string) (map[int64]string, error)
	GetActiveCount() int
	GetActiveIDs() []int64
	// Subscribe returns a channel that receives queue state change events.
	Subscribe() chan queue.Event
	// Unsubscribe removes a previously subscribed channel.
	Unsubscribe(ch chan queue.Event)
}

// New creates a new API handler container.
func New(st *store.SQLiteStore, ircMgr IRCManager, queueMgr QueueManager,
	searchAgg *searchagg.Aggregator, sseHub *sse.Hub,
	logBroadcaster *logging.LogBroadcaster,
	cfg *config.Config, logger *logging.Logger) *API {
	return &API{
		Store:            st,
		IRCManager:       ircMgr,
		QueueManager:     queueMgr,
		SearchAggregator: searchAgg,
		SSEHub:           sseHub,
		LogBroadcaster:   logBroadcaster,
		Config:           cfg,
		Logger:           logger,
		StartTime:        time.Now(),
	}
}

// =========================================================================
// Standard error response
// =========================================================================

// ErrorResponse is the standard JSON error response body.
type ErrorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	} `json:"error"`
}

func newErrorResponse(code, msg, reqID string) ErrorResponse {
	var e ErrorResponse
	e.Error.Code = code
	e.Error.Message = msg
	e.Error.RequestID = reqID
	return e
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	resp := newErrorResponse(code, msg, "")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// =========================================================================
// Middleware
// =========================================================================

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher to support SSE streaming
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// CORS returns middleware that sets CORS headers.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging returns middleware that logs each request.
// HTTP request logs are at DEBUG level since they are high-frequency.
func Logging(logger *logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)
			reqID := rw.Header().Get("X-Request-ID")
			if reqID == "" {
				reqID = "-"
			}

			logger.Debugf("%s %s %d %s [%s]",
				r.Method, r.URL.Path, rw.status, duration.Round(time.Millisecond), reqID)
		})
	}
}

// contextKey is used for storing values in request context to avoid
// collisions with built-in string keys.
type contextKey string

const requestIDKey contextKey = "request-id"

// RequestID returns middleware that injects a unique request ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", id)

		// Store request ID in context so handlers can access it
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// =========================================================================
// Helpers
// =========================================================================

// parseID parses an int64 from a string (URL param).
func parseID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}

// parsePageParams extracts page and pageSize from query string.
func parsePageParams(r *http.Request) (page, pageSize int) {
	page = 1
	pageSize = 50
	if p := r.URL.Query().Get("page"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &page)
	}
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		_, _ = fmt.Sscanf(ps, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	return
}

// parseInt parses an integer from a string.
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// parseInt64 parses an int64 from a string.
func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// logAndError is a helper to log and write an error response.
func (a *API) logAndError(w http.ResponseWriter, status int, code, msg string) {
	a.Logger.Errorf("ERROR: %s: %s", code, msg)
	writeError(w, status, code, msg)
}
