// Package store provides persistence for the xdcc-server using SQLite.
package store

import "time"

// Store defines the interface for all persistence operations.
type Store interface {
	// ---- Lifecycle ----
	Close() error

	// ---- Schema Migrations ----
	Migrate() error
	CurrentSchemaVersion() (int, error)

	// ---- IRC Servers ----
	AddServer(s ServerRecord) (int64, error)
	GetServer(id int64) (*ServerRecord, error)
	ListServers() ([]ServerRecord, error)
	UpdateServer(s ServerRecord) error
	DeleteServer(id int64) error
	SetServerStatus(id int64, status string) error
	SetServerConnected(id int64) error
	IncrementServerRetry(id int64) error

	// ---- IRC Channels ----
	AddChannel(c ChannelRecord) (int64, error)
	GetChannelsByServer(serverID int64) ([]ChannelRecord, error)
	GetChannelsByServerAndName(serverID int64, name string) (*ChannelRecord, error)
	UpdateChannel(c ChannelRecord) error
	DeleteChannel(id int64) error
	SetChannelJoined(id int64, joined bool) error
	UpdateChannelTopic(id int64, topic string) error
	GetAutoJoinChannels() ([]ChannelRecord, error)

	// ---- Downloads ----
	EnqueueDownload(d DownloadRecord) (int64, error)
	GetDownload(id int64) (*DownloadRecord, error)
	GetQueue() ([]DownloadRecord, error)
	GetQueueByChannel(channel string) ([]DownloadRecord, error)
	GetActiveDownloads() ([]DownloadRecord, error)
	GetPendingByChannel(channel string) ([]DownloadRecord, error)
	UpdateDownloadProgress(id int64, progressBytes int64, speedBPS int64) error
	MarkDownloadStarted(id int64) error
	MarkDownloadCompleted(id int64) error
	MarkDownloadFailed(id int64, errMsg string) error
	MarkDownloadSkipped(id int64) error
	MarkDownloadPaused(id int64) error
	MarkDownloadRetry(id int64, newStatus string) error
	DeleteDownload(id int64) error
	RetryDownload(id int64) error
	GetDownloadHistory(page, pageSize int) ([]DownloadRecord, int, error)
	RecoverDownloadsOnStartup() ([]DownloadRecord, error)
	RequeueDownload(id int64) error
	SetDownloadPriority(id int64, priority int) error
	BulkActionDownloads(ids []int64, action string) (map[int64]string, error)
	FindDuplicateDownload(bot, serverAddress string, packNumber int) (*DownloadRecord, error)
	GetDownloadByBotMessage(bot, packMessage string) (*DownloadRecord, error)

	// ---- Search Cache ----
	SetSearchCache(entry SearchCacheEntry) error
	GetSearchCache(queryKey, provider string) (*SearchCacheEntry, error)
	DeleteExpiredSearchCache(staleBefore time.Time) error

	// ---- Search Presets ----
	AddSearchPreset(p SearchPreset) (int64, error)
	GetSearchPreset(id int64) (*SearchPreset, error)
	ListSearchPresets() ([]SearchPreset, error)
	UpdateSearchPreset(p SearchPreset) error
	DeleteSearchPreset(id int64) error
	SetDefaultSearchPreset(id int64) error

	// ---- Watchlists ----
	AddWatchlist(w Watchlist) (int64, error)
	GetWatchlist(id int64) (*Watchlist, error)
	ListWatchlists() ([]Watchlist, error)
	UpdateWatchlist(w Watchlist) error
	DeleteWatchlist(id int64) error
	SetWatchlistChecked(id int64, fingerprint string) error
	SetWatchlistNotified(id int64) error
	GetEnabledWatchlists() ([]Watchlist, error)

	// ---- Provider Stats ----
	RecordProviderStats(s ProviderStats) error
	GetProviderStats(provider string, since time.Time) ([]ProviderStats, error)
	GetAllProviderStats(since time.Time) (map[string][]ProviderStats, error)

	// ---- Cleanup ----
	CleanupOldDownloads(retentionDays int) (int, error) // returns number of deleted records
	RunCleanup(retentionDays int, cleanupInterval time.Duration) (stopCh chan struct{}, doneCh chan struct{}, err error)
	Vacuum() error

	// ---- Backup / Export / Import ----
	ExportData() (*ExportData, error)
	ImportData(data *ExportData) error
	BackupDatabase(destPath string) error
}

// ---------------------------------------------------------------------------
// ExportData — used for export/import of config + state
// ---------------------------------------------------------------------------

// ExportData holds a snapshot of database state for export/import.
type ExportData struct {
	SchemaVersion int                `json:"schema_version"`
	ExportedAt    time.Time          `json:"exported_at"`
	Servers       []ServerRecord     `json:"servers,omitempty"`
	Channels      []ChannelRecord    `json:"channels,omitempty"`
	Downloads     []DownloadRecord   `json:"downloads,omitempty"`
	SearchPresets []SearchPreset     `json:"search_presets,omitempty"`
	Watchlists    []Watchlist        `json:"watchlists,omitempty"`
}
