// Package api implements the REST API for the xdcc-server (Fase 6).
//
// Uses go-chi/chi v5 for routing. Handlers reference concrete types from
// internal/store, internal/searchagg, and internal/config to avoid type
// assertion boilerplate.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"xdcc-go/internal/config"
	"xdcc-go/internal/searchagg"
	"xdcc-go/internal/store"
)

// =========================================================================
// API — dependency container
// =========================================================================

// API holds all dependencies needed by the HTTP handlers.
type API struct {
	Store             *store.SQLiteStore
	IRCManager        IRCManager
	QueueManager      QueueManager
	SearchAggregator  *searchagg.Aggregator
	Config            *config.Config
	Logger            *log.Logger
	StartTime         time.Time
}

// IRCManager defines the subset of ircmanager.Manager methods used by handlers.
type IRCManager interface {
	GetServers() []store.ServerRecord
	ConnectServerByID(id int64) error
	DisconnectServer(id int64) error
	JoinChannel(serverID int64, channel string) error
	LeaveChannel(serverID int64, channel string) error
	GetChannels(serverID int64) []store.ChannelRecord
	GetChannelTopic(serverID int64, channel string) (string, error)
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
}

// New creates a new API handler container.
func New(st *store.SQLiteStore, ircMgr IRCManager, queueMgr QueueManager,
	searchAgg *searchagg.Aggregator, cfg *config.Config, logger *log.Logger) *API {
	return &API{
		Store:            st,
		IRCManager:       ircMgr,
		QueueManager:     queueMgr,
		SearchAggregator: searchAgg,
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
	json.NewEncoder(w).Encode(resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
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
func Logging(logger *log.Logger) func(http.Handler) http.Handler {
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

			logger.Printf("%s %s %d %s [%s]",
				r.Method, r.URL.Path, rw.status, duration.Round(time.Millisecond), reqID)
		})
	}
}

// RequestID returns middleware that injects a unique request ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
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
		fmt.Sscanf(p, "%d", &page)
	}
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		fmt.Sscanf(ps, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	return
}

// logAndError is a helper to log and write an error response.
func (a *API) logAndError(w http.ResponseWriter, status int, code, msg string) {
	a.Logger.Printf("ERROR: %s: %s", code, msg)
	writeError(w, status, code, msg)
}
