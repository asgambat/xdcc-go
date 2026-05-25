package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ---------------------------------------------------------------------------
// Export — 2.8: Export configuration and state
// ---------------------------------------------------------------------------

// ExportData holds a complete snapshot of database state for export.
// (Defined in store.go for consistency.)

// ExportDataVersion is the current export format version.
const ExportDataVersion = 1

// ExportData compiles a complete export of all relevant data from the store.
func (s *SQLiteStore) ExportData(ctx context.Context) (*ExportData, error) {
	version, err := getCurrentVersion(s.db)
	if err != nil {
		return nil, fmt.Errorf("getting schema version for export: %w", err)
	}

	servers, err := s.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing servers for export: %w", err)
	}

	var allChannels []ChannelRecord
	for _, srv := range servers {
		channels, err := s.GetChannelsByServer(ctx, srv.ID)
		if err != nil {
			return nil, fmt.Errorf("getting channels for server %d: %w", srv.ID, err)
		}
		allChannels = append(allChannels, channels...)
	}

	// Export downloads that are still relevant (queued, downloading, paused)
	queue, err := s.GetQueue(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting queue for export: %w", err)
	}

	presets, err := s.ListSearchPresets(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing presets for export: %w", err)
	}

	watchlists, err := s.ListWatchlists(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing watchlists for export: %w", err)
	}

	export := &ExportData{
		SchemaVersion: version,
		ExportedAt:    time.Now(),
		Servers:       servers,
		Channels:      allChannels,
		Downloads:     queue,
		SearchPresets: presets,
		Watchlists:    watchlists,
	}

	return export, nil
}

// ExportToFile exports the database state to a JSON file.
func (s *SQLiteStore) ExportToFile(ctx context.Context, path string) error {
	data, err := s.ExportData(ctx)
	if err != nil {
		return err
	}

	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling export data: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("writing export file %s: %w", path, err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Import — 2.8: Import configuration and state
// ---------------------------------------------------------------------------

// ImportData imports previously exported data into the store.
// It validates schema compatibility before importing.
func (s *SQLiteStore) ImportData(ctx context.Context, data *ExportData) error {
	if data == nil {
		return fmt.Errorf("import data is nil")
	}

	// Validate schema version compatibility
	if data.SchemaVersion > currentSchemaVersion {
		return fmt.Errorf(
			"export schema version %d is newer than current %d",
			data.SchemaVersion, currentSchemaVersion,
		)
	}

	// Import servers
	for _, srv := range data.Servers {
		id, err := s.AddServer(ctx, srv)
		if err != nil {
			return fmt.Errorf("importing server %s:%d: %w", srv.Address, srv.Port, err)
		}

		// Import channels for this server
		for _, ch := range data.Channels {
			if ch.ServerID == srv.ID {
				ch.ServerID = id
				if _, err := s.AddChannel(ctx, ch); err != nil {
					return fmt.Errorf("importing channel %s for server %s: %w", ch.Name, srv.Address, err)
				}
			}
		}
	}

	// Import downloads (reset to queued status)
	for _, d := range data.Downloads {
		d.Status = DownloadStatusQueued
		d.ProgressBytes = 0
		d.SpeedBPS = 0
		d.StartedAt = nil
		d.CompletedAt = nil
		if _, err := s.EnqueueDownload(ctx, d); err != nil {
			return fmt.Errorf("importing download %s from %s: %w", d.Filename, d.Bot, err)
		}
	}

	// Import presets
	for _, p := range data.SearchPresets {
		p.ID = 0 // Reset ID so auto-increment generates new ones
		if _, err := s.AddSearchPreset(ctx, p); err != nil {
			return fmt.Errorf("importing preset %s: %w", p.Name, err)
		}
	}

	// Import watchlists
	for _, w := range data.Watchlists {
		w.ID = 0
		w.LastCheckedAt = nil
		w.LastMatchFingerprint = ""
		w.LastNotifiedAt = nil
		if _, err := s.AddWatchlist(ctx, w); err != nil {
			return fmt.Errorf("importing watchlist %s: %w", w.Name, err)
		}
	}

	return nil
}

// ImportFromFile imports state from a JSON export file.
func (s *SQLiteStore) ImportFromFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading import file %s: %w", path, err)
	}

	var export ExportData
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("parsing import file: %w", err)
	}

	return s.ImportData(ctx, &export)
}

// ---------------------------------------------------------------------------
// Database backup — 2.8
// ---------------------------------------------------------------------------

// BackupDatabase creates a snapshot backup of the SQLite database to destPath.
// Uses SQLite's VACUUM INTO for a consistent snapshot of a live database.
func (s *SQLiteStore) BackupDatabase(ctx context.Context, destPath string) error {
	return backupDB(ctx, s.db, destPath)
}
