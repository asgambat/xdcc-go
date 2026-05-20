package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Router creates and configures the chi router with all API routes (Fase 6.1).
func (a *API) Router() http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(CORS)
	r.Use(RequestID)
	r.Use(Logging(a.Logger))
	r.Use(chimw.Recoverer)

	// =====================================================================
	// Health / readiness / version
	// =====================================================================
	r.Get("/healthz", a.handleHealthz)
	r.Get("/readyz", a.handleReadyz)
	r.Get("/api/version", a.handleVersion)

	// =====================================================================
	// Servers
	// =====================================================================
	r.Route("/api/servers", func(r chi.Router) {
		r.Get("/", a.handleListServers)                   // GET  /api/servers
		r.Post("/", a.handleConnectServer)                // POST /api/servers
		r.Route("/{serverID}", func(r chi.Router) {
			r.Delete("/", a.handleDisconnectServer)         // DELETE /api/servers/:id
			r.Get("/channels", a.handleListChannels)        // GET  /api/servers/:id/channels
			r.Post("/channels", a.handleJoinChannel)        // POST /api/servers/:id/channels
			r.Route("/channels/{channelName}", func(r chi.Router) {
				r.Delete("/", a.handleLeaveChannel)           // DELETE /api/servers/:id/channels/:name
				r.Get("/topic", a.handleGetChannelTopic)      // GET  /api/servers/:id/channels/:name/topic
				r.Patch("/", a.handleUpdateChannel)           // PATCH /api/servers/:id/channels/:name
			})
		})
	})

	// =====================================================================
	// Downloads
	// =====================================================================
	r.Route("/api/downloads", func(r chi.Router) {
		r.Get("/", a.handleListDownloads)                  // GET  /api/downloads
		r.Post("/", a.handleEnqueueDownload)               // POST /api/downloads
		r.Get("/history", a.handleDownloadHistory)         // GET  /api/downloads/history
		r.Post("/bulk", a.handleBulkDownloads)             // POST /api/downloads/bulk
		r.Route("/{downloadID}", func(r chi.Router) {
			r.Get("/", a.handleGetDownload)                  // GET  /api/downloads/:id
			r.Delete("/", a.handleRemoveDownload)             // DELETE /api/downloads/:id
			r.Post("/pause", a.handlePauseDownload)           // POST /api/downloads/:id/pause
			r.Post("/resume", a.handleResumeDownload)         // POST /api/downloads/:id/resume
			r.Post("/retry", a.handleRetryDownload)           // POST /api/downloads/:id/retry
			r.Patch("/position", a.handleSetDownloadPosition) // PATCH /api/downloads/:id/position
		})
	})

	// =====================================================================
	// Search
	// =====================================================================
	r.Get("/api/search", a.handleSearch)                     // GET /api/search

	// Search presets
	r.Route("/api/search/presets", func(r chi.Router) {
		r.Get("/", a.handleListPresets)                    // GET  /api/search/presets
		r.Post("/", a.handleCreatePreset)                  // POST /api/search/presets
		r.Route("/{presetID}", func(r chi.Router) {
			r.Put("/", a.handleUpdatePreset)                 // PUT  /api/search/presets/:id
			r.Delete("/", a.handleDeletePreset)               // DELETE /api/search/presets/:id
		})
	})

	// Watchlists
	r.Route("/api/watchlists", func(r chi.Router) {
		r.Get("/", a.handleListWatchlists)                 // GET  /api/watchlists
		r.Post("/", a.handleCreateWatchlist)               // POST /api/watchlists
		r.Route("/{watchlistID}", func(r chi.Router) {
			r.Put("/", a.handleUpdateWatchlist)              // PUT  /api/watchlists/:id
			r.Delete("/", a.handleDeleteWatchlist)            // DELETE /api/watchlists/:id
			r.Post("/run", a.handleRunWatchlist)              // POST /api/watchlists/:id/run
		})
	})

	// Provider management
	r.Get("/api/search/providers", a.handleGetProviders)     // GET  /api/search/providers
	r.Patch("/api/search/providers/{providerName}", a.handlePatchProvider) // PATCH /api/search/providers/:name

	// =====================================================================
	// XDCC quick-add parser
	// =====================================================================
	r.Post("/api/xdcc/parse", a.handleParseXDCC)             // POST /api/xdcc/parse

	// =====================================================================
	// Configuration
	// =====================================================================
	r.Get("/api/config", a.handleGetConfig)                  // GET  /api/config
	r.Put("/api/config", a.handleUpdateConfig)               // PUT  /api/config

	// =====================================================================
	// System / Admin
	// =====================================================================
	r.Get("/api/stats", a.handleStats)                       // GET  /api/stats
	r.Get("/api/status", a.handleStatus)                     // GET  /api/status
	r.Post("/api/admin/export", a.handleAdminExport)         // POST /api/admin/export
	r.Post("/api/admin/import", a.handleAdminImport)         // POST /api/admin/import

	// =====================================================================
	// SSE events stream (Fase 7.1)
	// =====================================================================
	r.Get("/api/events", a.handleEvents)                     // GET /api/events

	// =====================================================================
	// Setup wizard
	// =====================================================================
	r.Get("/api/setup/status", a.handleSetupStatus)          // GET  /api/setup/status
	r.Post("/api/setup/bootstrap", a.handleSetupBootstrap)   // POST /api/setup/bootstrap

	// =====================================================================
	// Frontend SPA — serve static files with fallback to index.html
	// =====================================================================
	// Mount the frontend file server for all non-API routes.
	// API routes must be registered BEFORE this catch-all.
	r.Get("/*", a.handleFrontend)

	return r
}
