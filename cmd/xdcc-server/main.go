// xdcc-server is the daemon that manages persistent IRC connections, download
// queues, and exposes a REST API + web UI for remote control.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"xdcc-go/internal/api"
	"xdcc-go/internal/config"
	"xdcc-go/internal/ircmanager"
	"xdcc-go/internal/queue"
	"xdcc-go/internal/searchagg"
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

			// Setup logger
			logger := log.New(os.Stderr, "[xdcc-server] ", log.LstdFlags|log.Lmsgprefix)
			logger.Printf("starting xdcc-server on port %d", cfg.HTTP.Port)

			// Ensure download directories exist
			for _, dir := range []string{cfg.Download.TempDir, cfg.Download.DestDir} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return fmt.Errorf("creating directory %s: %w", dir, err)
				}
			}

			// Initialize SQLite store
			dbDir := filepath.Dir(cfg.Download.TempDir)
			dbPath := filepath.Join(dbDir, "xdcc-server.db")
			logger.Printf("initializing database at %s", dbPath)

			st, err := store.NewSQLiteStore(dbPath)
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer st.Close()

			// Run schema migrations
			if err := st.Migrate(); err != nil {
				return fmt.Errorf("running database migrations: %w", err)
			}
			logger.Printf("database migrations complete (schema v%d)", currentSchemaVersion(st))

			// Recovery: requeue downloads stuck in 'downloading' status
			recovered, err := st.RecoverDownloadsOnStartup()
			if err != nil {
				logger.Printf("WARNING: download recovery failed: %v", err)
			} else if len(recovered) > 0 {
				logger.Printf("recovered %d downloads from previous session", len(recovered))
			}

			// Filesystem reconciliation
			actions, err := st.ReconcileFileSystem(cfg.Download.TempDir, "move", "")
			if err != nil {
				logger.Printf("WARNING: filesystem reconciliation failed: %v", err)
			} else {
				for _, action := range actions {
					logger.Printf("reconciliation: %s", action)
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
			stopCleanup, err := st.RunCleanup(retentionDays, cleanupInterval)
			if err != nil {
				logger.Printf("WARNING: starting cleanup goroutine failed: %v", err)
			} else {
				defer close(stopCleanup)
			}

	// Start IRC connection manager
	ircMgr := ircmanager.New(st, cfg, logger)
	if err := ircMgr.Start(); err != nil {
		return fmt.Errorf("starting IRC manager: %w", err)
	}
	defer ircMgr.Stop()
	logger.Printf("IRC manager started with %d default server(s)", len(cfg.IRC.DefaultServers))

	// Start download queue manager
	queueMgr := queue.New(st, cfg, logger)
	if err := queueMgr.Start(); err != nil {
		return fmt.Errorf("starting queue manager: %w", err)
	}
	defer queueMgr.Stop()
	logger.Printf("queue manager started (max_parallel=%d)", cfg.Download.MaxParallelTotal)

	// Start search aggregator
	searchAgg := searchagg.New(st, &cfg.Search, logger)
	logger.Printf("search aggregator ready (%d provider(s), cache=%v)",
		len(cfg.Search.EnabledProviders), cfg.Search.Cache.Enabled)

	// Build REST API and wire it into the HTTP server
	apiHandler := api.New(st, ircMgr, queueMgr, searchAgg, cfg, logger)
	mux := apiHandler.Router()

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler: mux,
	}

			// Graceful shutdown
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

			go func() {
				logger.Printf("HTTP server listening on :%d", cfg.HTTP.Port)
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Fatalf("HTTP server error: %v", err)
				}
			}()

			sig := <-quit
			logger.Printf("received signal %v, shutting down...", sig)

			// Create shutdown context with 15s timeout
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			if err := srv.Shutdown(ctx); err != nil {
				logger.Printf("HTTP server forced shutdown: %v", err)
			}

			logger.Printf("server stopped")
			return nil
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
