// xdcc-server is the daemon that manages persistent IRC connections, download
// queues, and exposes a REST API + web UI for remote control.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"xdcc-go/internal/api"
	"xdcc-go/internal/config"
	"xdcc-go/internal/ircmanager"
	"xdcc-go/internal/logging"
	"xdcc-go/internal/queue"
	"xdcc-go/internal/search"
	"xdcc-go/internal/searchagg"
	"xdcc-go/internal/sse"
	"xdcc-go/internal/store"
)

func main() {
	var (
		configPath  string
		port        int
		downloadDir string
		tempDir     string
	)

	cmd := &cobra.Command{
		Use:   "xdcc-server",
		Short: "XDCC download daemon with REST API and web UI",
		Long: `xdcc-server is a persistent daemon that manages IRC connections and
downloads. It exposes a REST API and serves a web UI for remote control.

Configuration is loaded from config.yaml. Environment variables and CLI flags
take precedence over the config file.

Start the server:
  xdcc-server

Specify a custom config file:
  xdcc-server --config /path/to/config.yaml

Override the HTTP port:
  xdcc-server --port 9090

See config.yaml in the project root for all available settings.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build flag overrides
			flagOverrides := &config.FlagOverrides{}
			if cmd.Flags().Changed("port") {
				flagOverrides.Port = &port
			}
			if cmd.Flags().Changed("download-dir") {
				flagOverrides.DownloadDir = &downloadDir
			}
			if cmd.Flags().Changed("temp-dir") {
				flagOverrides.TempDir = &tempDir
			}

			// Load configuration
			cfgPath := configPath
			if cfgPath == "" {
				cfgPath = "config.yaml"
			}
			cfg, err := config.Load(cfgPath, flagOverrides)
			if err != nil {
				return fmt.Errorf("loading configuration: %w", err)
			}

			// Setup logger with configurable level
			logLevel, err := logging.ParseLevel(cfg.Logging.Level)
			if err != nil {
				logLevel = logging.LevelInfo
			}
			logger := logging.New(logLevel, cfg.Logging.FilePath, 10) // 10 MB rotation
			defer logger.Close()
			logger.Infof("starting xdcc-server on port %d", cfg.HTTP.Port)

			// Create a stdlib *log.Logger adapter at INFO level for components
			// that still use the old interface (ircmanager, queue, searchagg).
			// This ensures those components' logs are also level-filtered.
			stdLogger := log.New(logger.Writer(logging.LevelInfo), "", 0)

			// Ensure download directories exist
			for _, dir := range []string{cfg.Download.TempDir, cfg.Download.DestDir} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return fmt.Errorf("creating directory %s: %w", dir, err)
				}
			}

			// Initialize SQLite store
			dbDir := filepath.Dir(cfg.Download.TempDir)
			dbPath := filepath.Join(dbDir, "xdcc-server.db")
			logger.Infof("initializing database at %s", dbPath)

			st, err := store.NewSQLiteStore(dbPath)
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer st.Close()

			// Run schema migrations
			if err := st.Migrate(); err != nil {
				return fmt.Errorf("running database migrations: %w", err)
			}
			logger.Infof("database migrations complete (schema v%d)", currentSchemaVersion(st))

			// Recovery: requeue downloads stuck in 'downloading' status
			recovered, err := st.RecoverDownloadsOnStartup()
			if err != nil {
				logger.Warnf("download recovery failed: %v", err)
			} else if len(recovered) > 0 {
				logger.Infof("recovered %d downloads from previous session", len(recovered))
			}

			// Filesystem reconciliation
			actions, err := st.ReconcileFileSystem(cfg.Download.TempDir, "move", "")
			if err != nil {
				logger.Warnf("filesystem reconciliation failed: %v", err)
			} else {
				for _, action := range actions {
					logger.Infof("reconciliation: %s", action)
				}
			}

			// Start periodic cleanup
			cleanupInterval, err := cfg.ParseCleanupInterval()
			if err != nil {
				cleanupInterval = 12 * time.Hour
			}
			retentionDays, err := cfg.ParseDownloadsRetention()
			if err != nil {
				retentionDays = 30
			}
			stopCleanup, cleanupDone, err := st.RunCleanup(retentionDays, cleanupInterval)
			if err != nil {
				logger.Warnf("starting cleanup goroutine failed: %v", err)
			} else {
				defer func() {
					close(stopCleanup)
					select {
					case <-cleanupDone:
						logger.Infof("cleanup goroutine stopped")
					case <-time.After(3 * time.Second):
						logger.Warnf("cleanup goroutine did not stop within 3s")
					}
				}()
			}

			// Start IRC connection manager
			ircMgr := ircmanager.New(st, cfg, stdLogger)
			if err := ircMgr.Start(); err != nil {
				return fmt.Errorf("starting IRC manager: %w", err)
			}
			defer ircMgr.Stop()
			logger.Infof("IRC manager started with %d default server(s)", len(cfg.IRC.DefaultServers))

			// Start download queue manager
			queueMgr := queue.New(st, cfg, stdLogger)
			queueMgr.SetIRCManager(ircMgr) // Connect IRC Manager for persistent connections
			if err := queueMgr.Start(); err != nil {
				return fmt.Errorf("starting queue manager: %w", err)
			}
			defer queueMgr.Stop()
			if cfg.Download.StartupDelayMinutes > 0 {
				logger.Infof("queue manager started (max_parallel=%d, startup_delay=%dm, persistent_irc=enabled)",
					cfg.Download.MaxParallelTotal, cfg.Download.StartupDelayMinutes)
			} else {
				logger.Infof("queue manager started (max_parallel=%d, persistent_irc=enabled)", cfg.Download.MaxParallelTotal)
			}

			// Start search aggregator
			searchAgg := searchagg.New(st, &cfg.Search, stdLogger)
			if err := searchAgg.Start(context.Background()); err != nil {
				return fmt.Errorf("starting search aggregator: %w", err)
			}
			defer searchAgg.Stop()
			providerCount := len(cfg.Search.EnabledProviders)
			if providerCount == 0 {
				providerCount = len(search.AvailableEngines())
			}
			logger.Infof("search aggregator ready (%d provider(s), cache=%v)",
				providerCount, cfg.Search.Cache.Enabled)

		// Start SSE event hub (Fase 7)
		sseHub := sse.NewHub(100) // buffer last 100 events
		logger.Infof("SSE hub started (buffer=100)")

		// Start log broadcaster: streams log lines to SSE clients (Fase 10.1)
		logBroadcaster := logging.NewLogBroadcaster(sseHub)
		logger.AddWriter(logBroadcaster)
		logger.Infof("log broadcaster started (buffer=%d lines)", logging.MaxLogBufferLines)

		// Track event forwarding goroutines for clean shutdown
		var eventWg sync.WaitGroup
		eventCtx, cancelEvents := context.WithCancel(context.Background())
		defer cancelEvents()

			// Wire IRC manager events into SSE hub (Fase 7.2)
			ircEventCh := ircMgr.Subscribe()
			defer ircMgr.Unsubscribe(ircEventCh)
			eventWg.Add(1)
			go func() {
				defer eventWg.Done()
				for {
					select {
					case <-eventCtx.Done():
						// Shutdown signal - drain remaining events with timeout
						timeout := time.After(100 * time.Millisecond)
						for {
							select {
							case evt, ok := <-ircEventCh:
								if !ok {
									return
								}
								// Try to publish remaining events (best effort)
								sseHub.Publish(string(evt.Type), map[string]interface{}{
									"server_id":   evt.ServerID,
									"server_addr": evt.ServerAddr,
									"channel":     evt.Channel,
									"topic":       evt.Topic,
									"timestamp":   evt.Timestamp,
								})
							case <-timeout:
								return
							}
						}
					case evt, ok := <-ircEventCh:
						if !ok {
							return
						}
						sseHub.Publish(string(evt.Type), map[string]interface{}{
							"server_id":   evt.ServerID,
							"server_addr": evt.ServerAddr,
							"channel":     evt.Channel,
							"topic":       evt.Topic,
							"timestamp":   evt.Timestamp,
						})
					}
				}
			}()

			// Wire queue manager events into SSE hub (Fase 7.3)
			queueEventCh := queueMgr.Subscribe()
			defer queueMgr.Unsubscribe(queueEventCh)
			eventWg.Add(1)
			go func() {
				defer eventWg.Done()
				for {
					select {
					case <-eventCtx.Done():
						// Shutdown signal - drain remaining events with timeout
						timeout := time.After(100 * time.Millisecond)
						for {
							select {
							case evt, ok := <-queueEventCh:
								if !ok {
									return
								}
								// Try to publish remaining events (best effort)
								sseHub.Publish(string(evt.Type), map[string]interface{}{
									"download_id":    evt.DownloadID,
									"bot":            evt.Bot,
									"server_address": evt.ServerAddress,
									"channel":        evt.Channel,
									"filename":       evt.Filename,
									"progress_bytes": evt.ProgressBytes,
									"file_size":      evt.FileSize,
									"speed_bps":      evt.SpeedBPS,
									"error_message":  evt.ErrorMessage,
									"timestamp":      evt.Timestamp,
								})
							case <-timeout:
								return
							}
						}
					case evt, ok := <-queueEventCh:
						if !ok {
							return
						}
						sseHub.Publish(string(evt.Type), map[string]interface{}{
							"download_id":    evt.DownloadID,
							"bot":            evt.Bot,
							"server_address": evt.ServerAddress,
							"channel":        evt.Channel,
							"filename":       evt.Filename,
							"progress_bytes": evt.ProgressBytes,
							"file_size":      evt.FileSize,
							"speed_bps":      evt.SpeedBPS,
							"error_message":  evt.ErrorMessage,
							"timestamp":      evt.Timestamp,
						})
					}
				}
			}()

		// Build REST API and wire it into the HTTP server
		apiHandler := api.New(st, ircMgr, queueMgr, searchAgg, sseHub, logBroadcaster, cfg, logger)
			mux := apiHandler.Router()

			// Create global shutdown context for request cancellation (Phase 1.1)
			// This context will be the parent of all HTTP request contexts
			globalShutdownCtx, globalShutdownCancel := context.WithCancel(context.Background())
			defer globalShutdownCancel()

			srv := &http.Server{
				Addr:    fmt.Sprintf(":%d", cfg.HTTP.Port),
				Handler: mux,
				// BaseContext provides the base context for all incoming requests (Phase 1.2)
				// When we cancel globalShutdownCtx, all active request contexts are cancelled
				BaseContext: func(net.Listener) context.Context {
					return globalShutdownCtx
				},
			}

			// Graceful shutdown (Fase 9.1)
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

			go func() {
				logger.Infof("HTTP server listening on :%d", cfg.HTTP.Port)
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Errorf("HTTP server FATAL error: %v", err)
					os.Exit(1)
				}
			}()

			sig := <-quit
			logger.Infof("received signal %v, shutting down...", sig)

			// Create shutdown context with 15s timeout
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer shutdownCancel()

			// === Ordered Shutdown Sequence (Fase 9.1) ===

			// 0. Stop event forwarding loops FIRST (prevents deadlock)
			logger.Infof("shutdown: stopping event forwarding...")
			cancelEvents()
			eventStopDone := make(chan struct{})
			go func() {
				eventWg.Wait()
				close(eventStopDone)
			}()
			select {
			case <-eventStopDone:
				logger.Infof("shutdown: event forwarding stopped")
			case <-time.After(2 * time.Second):
				logger.Warnf("event forwarding did not stop within 2s")
			}

			// 1. Close SSE hub after event loops stopped
			logger.Infof("shutdown: closing SSE hub...")
			sseHub.Close()

			// 2. Cancel all active HTTP request contexts (Phase 1.3)
			logger.Infof("shutdown: cancelling all active requests...")
			globalShutdownCancel()
			time.Sleep(100 * time.Millisecond) // Give handlers time to see cancellation

			// 3. Stop the HTTP server (handlers should exit quickly now)
			logger.Infof("shutdown: stopping HTTP server...")
			if err := srv.Shutdown(shutdownCtx); err != nil {
				logger.Infof("shutdown: HTTP server forced shutdown: %v", err)
			}

			// 4. Cancel the search aggregator context (with timeout)
			logger.Infof("shutdown: stopping search aggregator...")
			stopWithTimeout("search aggregator", 2*time.Second, func() {
				searchAgg.Stop()
			}, logger)

			// 4. Cancel all active queue downloads (saves progress first, with timeout)
			logger.Infof("shutdown: stopping queue manager...")
			stopWithTimeout("queue manager", 10*time.Second, func() {
				queueMgr.Stop()
			}, logger)

			// 5. Disconnect all IRC servers with QUIT message (with timeout)
			logger.Infof("shutdown: disconnecting IRC servers...")
			stopWithTimeout("IRC manager", 5*time.Second, func() {
				ircMgr.Stop()
			}, logger)

			// 6. Run final cleanup save (with timeout)
			logger.Infof("shutdown: running final database cleanup...")
			stopWithTimeout("database cleanup", 3*time.Second, func() {
				_ = st.Vacuum()
			}, logger)

			logger.Infof("server stopped gracefully")

			// Force exit to ensure all goroutines are terminated
			// Some goroutines may not have shut down cleanly within timeouts
			os.Exit(0)
			return nil // Unreachable, but required by compiler
		},
	}

	// Flags
	cmd.Flags().StringVar(&configPath, "config", "config.yaml",
		"Path to configuration file (YAML)")
	cmd.Flags().IntVar(&port, "port", 0,
		"HTTP server port (overrides config.yaml)")
	cmd.Flags().StringVar(&downloadDir, "download-dir", "",
		"Destination directory for completed downloads (overrides config.yaml)")
	cmd.Flags().StringVar(&tempDir, "temp-dir", "",
		"Temporary directory for in-progress downloads (overrides config.yaml)")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// currentSchemaVersion is a helper to safely get the schema version for logging.
func currentSchemaVersion(st *store.SQLiteStore) int {
	v, err := st.CurrentSchemaVersion()
	if err != nil {
		return 0
	}
	return v
}

// stopWithTimeout executes a stop function with a timeout.
// If the function doesn't complete within the timeout, it logs a warning and continues.
func stopWithTimeout(name string, timeout time.Duration, fn func(), logger *logging.Logger) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
		// Completed successfully
	case <-time.After(timeout):
		logger.Warnf("%s stop exceeded timeout (%v), forcing shutdown", name, timeout)
	}
}
