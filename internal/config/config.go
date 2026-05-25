// Package config provides configuration loading and validation for the xdcc-server.
// Configuration is loaded from three sources, in increasing priority:
//  1. config.yaml (lowest priority)
//  2. Environment variables
//  3. CLI flags (highest priority)
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Config struct
// ---------------------------------------------------------------------------

// Config holds the complete server configuration.
type Config struct {
	IRC      IRCConfig      `yaml:"irc"`
	HTTP     HTTPConfig     `yaml:"http"`
	Download DownloadConfig `yaml:"download"`
	Search   SearchConfig   `yaml:"search"`
	Storage  StorageConfig  `yaml:"storage"`
	Logging  LoggingConfig  `yaml:"logging"`
	UI       UIConfig       `yaml:"ui"`
}

type IRCConfig struct {
	Nickname       string         `yaml:"nickname"        env:"XDCC_IRC_NICKNAME"`
	DefaultServers []ServerConfig `yaml:"default_servers"`
}

type ServerConfig struct {
	Address     string          `yaml:"address"`
	Port        int             `yaml:"port"`
	AutoConnect bool            `yaml:"auto_connect"`
	Channels    []ChannelConfig `yaml:"channels"`
}

type ChannelConfig struct {
	Name     string `yaml:"name"`
	AutoJoin bool   `yaml:"auto_join"`
}

type HTTPConfig struct {
	Port int `yaml:"port" env:"XDCC_HTTP_PORT"`
}

type DownloadConfig struct {
	TempDir             string `yaml:"temp_dir"               env:"XDCC_DOWNLOAD_TEMP_DIR"`
	DestDir             string `yaml:"dest_dir"               env:"XDCC_DOWNLOAD_DEST_DIR"`
	ConflictPolicy      string `yaml:"conflict_policy"        env:"XDCC_DOWNLOAD_CONFLICT_POLICY"`
	FailFallback        string `yaml:"fail_fallback"          env:"XDCC_DOWNLOAD_FAIL_FALLBACK"`
	MaxParallelTotal    int    `yaml:"max_parallel_total"     env:"XDCC_DOWNLOAD_MAX_PARALLEL"`
	MaxRateBPS          int64  `yaml:"max_rate_bps"           env:"XDCC_DOWNLOAD_MAX_RATE_BPS"`
	MinDiskSpace        int64  `yaml:"min_disk_space_bytes"   env:"XDCC_DOWNLOAD_MIN_DISK_SPACE"`
	MaxRetryAttempts    int    `yaml:"max_retry_attempts"     env:"XDCC_DOWNLOAD_MAX_RETRY"`
	StartupDelayMinutes int    `yaml:"startup_delay_minutes"  env:"XDCC_DOWNLOAD_STARTUP_DELAY_MINUTES"`
	ChannelJoinDelay    int    `yaml:"channel_join_delay"     env:"XDCC_DOWNLOAD_CHANNEL_JOIN_DELAY"`
}

type SearchConfig struct {
	ProviderTimeout  int               `yaml:"provider_timeout"  env:"XDCC_SEARCH_PROVIDER_TIMEOUT"`
	PageSize         int               `yaml:"page_size"         env:"XDCC_SEARCH_PAGE_SIZE"`
	EnabledProviders []string          `yaml:"enabled_providers"`
	Cache            SearchCacheConfig `yaml:"cache"`
}

type SearchCacheConfig struct {
	Enabled  bool          `yaml:"enabled"   env:"XDCC_SEARCH_CACHE_ENABLED"`
	FreshTTL time.Duration `yaml:"fresh_ttl"`
	StaleTTL time.Duration `yaml:"stale_ttl"`
}

type StorageConfig struct {
	DownloadsRetention string `yaml:"downloads_retention" env:"XDCC_STORAGE_DOWNLOADS_RETENTION"`
	CleanupInterval    string `yaml:"cleanup_interval"    env:"XDCC_STORAGE_CLEANUP_INTERVAL"`
}

type LoggingConfig struct {
	Level    string `yaml:"level"     env:"XDCC_LOGGING_LEVEL"`
	FilePath string `yaml:"file_path" env:"XDCC_LOGGING_FILE_PATH"`
}

type UIConfig struct {
	SetupCompleted bool `yaml:"setup_completed" env:"XDCC_UI_SETUP_COMPLETED"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

func DefaultConfig() *Config {
	return &Config{
		IRC: IRCConfig{
			Nickname: "xdcc-user",
			DefaultServers: []ServerConfig{
				{
					Address:     "irc.rizon.net",
					Port:        6667,
					AutoConnect: true,
					Channels: []ChannelConfig{
						{Name: "#news", AutoJoin: true},
					},
				},
				{
					Address:     "irc.williamgattone.it",
					Port:        6667,
					AutoConnect: false,
					Channels: []ChannelConfig{
						{Name: "#xdcc", AutoJoin: true},
					},
				},
			},
		},
		HTTP: HTTPConfig{
			Port: 8080,
		},
		Download: DownloadConfig{
			TempDir:             "./downloads/tmp",
			DestDir:             "./downloads/complete",
			ConflictPolicy:      "skip",
			FailFallback:        "suggest_only",
			MaxParallelTotal:    5,
			MaxRateBPS:          0,
			MinDiskSpace:        1 * 1024 * 1024 * 1024, // 1 GB default
			MaxRetryAttempts:    3,
			StartupDelayMinutes: 0,
			ChannelJoinDelay:    -1, // -1 = random 5-10s, 0 = no delay, >0 = fixed seconds
		},
		Search: SearchConfig{
			ProviderTimeout:  5,
			PageSize:         50,
			EnabledProviders: []string{},
			Cache: SearchCacheConfig{
				Enabled:  true,
				FreshTTL: 30 * time.Minute,
				StaleTTL: 24 * time.Hour,
			},
		},
		Storage: StorageConfig{
			DownloadsRetention: "30d",
			CleanupInterval:    "12h",
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "",
		},
		UI: UIConfig{
			SetupCompleted: false,
		},
	}
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

// Load reads configuration from config.yaml, overlays environment variables,
// then applies flag overrides. Returns the merged Config.
//
// Parameters:
//   - configPath: path to the YAML config file (empty = skip file loading)
//   - flagOverrides: optional overrides from CLI flags
func Load(configPath string, flagOverrides *FlagOverrides) (*Config, error) {
	cfg := DefaultConfig()

	// 1. Load from YAML file
	if configPath != "" {
		if err := cfg.loadFile(configPath); err != nil {
			return nil, fmt.Errorf("loading config file: %w", err)
		}
	}

	// 2. Overlay environment variables
	cfg.applyEnvOverrides()

	// 3. Apply CLI flag overrides
	if flagOverrides != nil {
		flagOverrides.apply(cfg)
	}

	// 4. Expand relative paths to absolute
	cfg.expandPaths()

	// 5. Validate
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// ---------------------------------------------------------------------------
// File loading
// ---------------------------------------------------------------------------

func (c *Config) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", path)
		}
		return fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Environment variable overlays
// ---------------------------------------------------------------------------

// applyEnvOverrides reads environment variables for fields tagged with `env:`
// and overrides the corresponding Config fields.
//
// Supported env vars:
//
//	XDCC_IRC_NICKNAME
//	XDCC_HTTP_PORT
//	XDCC_DOWNLOAD_TEMP_DIR
//	XDCC_DOWNLOAD_DEST_DIR
//	XDCC_DOWNLOAD_CONFLICT_POLICY
//	XDCC_DOWNLOAD_FAIL_FALLBACK
//	XDCC_DOWNLOAD_MAX_PARALLEL
//	XDCC_DOWNLOAD_MAX_RATE_BPS
//	XDCC_DOWNLOAD_MIN_DISK_SPACE
//	XDCC_DOWNLOAD_MAX_RETRY
//	XDCC_DOWNLOAD_STARTUP_DELAY_MINUTES
//	XDCC_SEARCH_PROVIDER_TIMEOUT
//	XDCC_SEARCH_PAGE_SIZE
//	XDCC_SEARCH_CACHE_ENABLED
//	XDCC_STORAGE_DOWNLOADS_RETENTION
//	XDCC_STORAGE_CLEANUP_INTERVAL
//	XDCC_LOGGING_LEVEL
//	XDCC_LOGGING_FILE_PATH
//	XDCC_UI_SETUP_COMPLETED
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("XDCC_IRC_NICKNAME"); v != "" {
		c.IRC.Nickname = v
	}
	if v := os.Getenv("XDCC_HTTP_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.HTTP.Port = port
		}
	}
	if v := os.Getenv("XDCC_DOWNLOAD_TEMP_DIR"); v != "" {
		c.Download.TempDir = v
	}
	if v := os.Getenv("XDCC_DOWNLOAD_DEST_DIR"); v != "" {
		c.Download.DestDir = v
	}
	if v := os.Getenv("XDCC_DOWNLOAD_CONFLICT_POLICY"); v != "" {
		c.Download.ConflictPolicy = v
	}
	if v := os.Getenv("XDCC_DOWNLOAD_FAIL_FALLBACK"); v != "" {
		c.Download.FailFallback = v
	}
	if v := os.Getenv("XDCC_DOWNLOAD_MAX_PARALLEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Download.MaxParallelTotal = n
		}
	}
	if v := os.Getenv("XDCC_DOWNLOAD_MAX_RATE_BPS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.Download.MaxRateBPS = n
		}
	}
	if v := os.Getenv("XDCC_DOWNLOAD_MIN_DISK_SPACE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.Download.MinDiskSpace = n
		}
	}
	if v := os.Getenv("XDCC_DOWNLOAD_MAX_RETRY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Download.MaxRetryAttempts = n
		}
	}
	if v := os.Getenv("XDCC_DOWNLOAD_STARTUP_DELAY_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Download.StartupDelayMinutes = n
		}
	}
	if v := os.Getenv("XDCC_DOWNLOAD_CHANNEL_JOIN_DELAY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Download.ChannelJoinDelay = n
		}
	}
	if v := os.Getenv("XDCC_SEARCH_PROVIDER_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Search.ProviderTimeout = n
		}
	}
	if v := os.Getenv("XDCC_SEARCH_PAGE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Search.PageSize = n
		}
	}
	if v := os.Getenv("XDCC_SEARCH_CACHE_ENABLED"); v != "" {
		c.Search.Cache.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("XDCC_STORAGE_DOWNLOADS_RETENTION"); v != "" {
		c.Storage.DownloadsRetention = v
	}
	if v := os.Getenv("XDCC_STORAGE_CLEANUP_INTERVAL"); v != "" {
		c.Storage.CleanupInterval = v
	}
	if v := os.Getenv("XDCC_LOGGING_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("XDCC_LOGGING_FILE_PATH"); v != "" {
		c.Logging.FilePath = v
	}
	if v := os.Getenv("XDCC_UI_SETUP_COMPLETED"); v != "" {
		c.UI.SetupCompleted = strings.EqualFold(v, "true") || v == "1"
	}
}

// ---------------------------------------------------------------------------
// FlagOverrides — values that can be set via CLI flags
// ---------------------------------------------------------------------------

// FlagOverrides holds optional CLI flag overrides that take highest priority.
type FlagOverrides struct {
	Port        *int
	DownloadDir *string
	TempDir     *string
	ConfigPath  *string
}

func (f *FlagOverrides) apply(c *Config) {
	if f == nil {
		return
	}
	if f.Port != nil {
		c.HTTP.Port = *f.Port
	}
	if f.DownloadDir != nil {
		c.Download.DestDir = *f.DownloadDir
	}
	if f.TempDir != nil {
		c.Download.TempDir = *f.TempDir
	}
}

// ---------------------------------------------------------------------------
// Path expansion
// ---------------------------------------------------------------------------

func (c *Config) expandPaths() {
	c.Download.TempDir = expandPath(c.Download.TempDir)
	c.Download.DestDir = expandPath(c.Download.DestDir)
	if c.Logging.FilePath != "" {
		c.Logging.FilePath = expandPath(c.Logging.FilePath)
	}
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		// Resolve relative to CWD
		wd, err := os.Getwd()
		if err == nil {
			return filepath.Join(wd, p)
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func (c *Config) validate() error {
	// IRC nickname
	if c.IRC.Nickname == "" {
		return fmt.Errorf("irc.nickname must not be empty")
	}

	// HTTP port
	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		return fmt.Errorf("http.port must be between 1 and 65535, got %d", c.HTTP.Port)
	}

	// Download directories
	if c.Download.TempDir == "" {
		return fmt.Errorf("download.temp_dir must not be empty")
	}
	if c.Download.DestDir == "" {
		return fmt.Errorf("download.dest_dir must not be empty")
	}

	// Conflict policy
	switch c.Download.ConflictPolicy {
	case "skip", "overwrite", "rename":
		// valid
	default:
		return fmt.Errorf("download.conflict_policy must be one of: skip, overwrite, rename (got %q)", c.Download.ConflictPolicy)
	}

	// Fail fallback
	switch c.Download.FailFallback {
	case "suggest_only", "auto_retry_best":
		// valid
	default:
		return fmt.Errorf("download.fail_fallback must be one of: suggest_only, auto_retry_best (got %q)", c.Download.FailFallback)
	}

	// Max parallel
	if c.Download.MaxParallelTotal < 1 {
		return fmt.Errorf("download.max_parallel_total must be at least 1, got %d", c.Download.MaxParallelTotal)
	}

	// Startup delay
	if c.Download.StartupDelayMinutes < 0 {
		return fmt.Errorf("download.startup_delay_minutes must be >= 0, got %d", c.Download.StartupDelayMinutes)
	}

	// Channel join delay: -1 = random, 0 = no delay, >0 = fixed
	if c.Download.ChannelJoinDelay < -1 {
		return fmt.Errorf("download.channel_join_delay must be >= -1 (random), got %d", c.Download.ChannelJoinDelay)
	}

	// Search
	if c.Search.ProviderTimeout < 1 {
		return fmt.Errorf("search.provider_timeout must be at least 1 second, got %d", c.Search.ProviderTimeout)
	}
	if c.Search.PageSize < 1 {
		return fmt.Errorf("search.page_size must be at least 1, got %d", c.Search.PageSize)
	}

	// Log level
	switch c.Logging.Level {
	case "debug", "info", "warn", "error", "":
		// valid
	default:
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error (got %q)", c.Logging.Level)
	}

	// Duration validation
	if _, err := parseDurationString(c.Storage.DownloadsRetention); err != nil {
		return fmt.Errorf("storage.downloads_retention: %w", err)
	}
	if _, err := parseDurationString(c.Storage.CleanupInterval); err != nil {
		return fmt.Errorf("storage.cleanup_interval: %w", err)
	}

	// Server configs
	for i, s := range c.IRC.DefaultServers {
		if s.Address == "" {
			return fmt.Errorf("irc.default_servers[%d].address must not be empty", i)
		}
		if s.Port < 1 || s.Port > 65535 {
			return fmt.Errorf("irc.default_servers[%d].port must be between 1 and 65535", i)
		}
		for j, ch := range s.Channels {
			if ch.Name == "" {
				return fmt.Errorf("irc.default_servers[%d].channels[%d].name must not be empty", i, j)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseDurationString parses a duration string like "30d", "12h", "45m".
func parseDurationString(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("duration string must not be empty")
	}

	// Handle "d" (days) suffix manually since Go's time.ParseDuration doesn't support it
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return time.ParseDuration(s)
}

// ParseDownloadsRetention parses the downloads retention string and returns
// the duration as a number of days.
func (c *Config) ParseDownloadsRetention() (int, error) {
	s := c.Storage.DownloadsRetention
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid downloads_retention %q: %w", s, err)
		}
		return days, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid downloads_retention %q: %w", s, err)
	}
	return int(d.Hours() / 24), nil
}

// ParseCleanupInterval parses the cleanup interval string into a time.Duration.
func (c *Config) ParseCleanupInterval() (time.Duration, error) {
	return parseDurationString(c.Storage.CleanupInterval)
}
