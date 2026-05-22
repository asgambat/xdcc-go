package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// SQLiteStore
// ---------------------------------------------------------------------------

// SQLiteStore implements the Store interface backed by SQLite.
type SQLiteStore struct {
	db     *sql.DB
	dbPath string
}

// NewSQLiteStore creates a new SQLiteStore and runs migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes well
	db.SetMaxIdleConns(1)

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	s := &SQLiteStore{
		db:     db,
		dbPath: dbPath,
	}

	return s, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB (for advanced use).
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// DBPath returns the path to the database file.
func (s *SQLiteStore) DBPath() string {
	return s.dbPath
}

// Migrate runs all pending schema migrations.
func (s *SQLiteStore) Migrate() error {
	return runMigrations(s.db, s.dbPath)
}

// =========================================================================
// IRC Servers
// =========================================================================

func (s *SQLiteStore) AddServer(srv ServerRecord) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO irc_servers (address, port, auto_connect, status, retry_count)
		 VALUES (?, ?, ?, ?, ?)`,
		srv.Address, srv.Port, boolToInt(srv.AutoConnect), srv.Status, srv.RetryCount,
	)
	if err != nil {
		return 0, fmt.Errorf("adding server: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetServer(id int64) (*ServerRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, address, port, auto_connect, status, last_connected_at, retry_count, created_at, updated_at
		 FROM irc_servers WHERE id = ?`, id,
	)
	return scanServer(row)
}

func (s *SQLiteStore) ListServers() ([]ServerRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, address, port, auto_connect, status, last_connected_at, retry_count, created_at, updated_at
		 FROM irc_servers ORDER BY address, port`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing servers: %w", err)
	}
	defer rows.Close()

	var servers []ServerRecord
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *srv)
	}
	if servers == nil {
		servers = []ServerRecord{}
	}
	return servers, rows.Err()
}

func (s *SQLiteStore) UpdateServer(srv ServerRecord) error {
	_, err := s.db.Exec(
		`UPDATE irc_servers SET address=?, port=?, auto_connect=?, status=?, updated_at=datetime('now')
		 WHERE id=?`,
		srv.Address, srv.Port, boolToInt(srv.AutoConnect), srv.Status, srv.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteServer(id int64) error {
	_, err := s.db.Exec(`DELETE FROM irc_servers WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) SetServerStatus(id int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE irc_servers SET status=?, updated_at=datetime('now') WHERE id=?`,
		status, id,
	)
	return err
}

func (s *SQLiteStore) SetServerConnected(id int64) error {
	_, err := s.db.Exec(
		`UPDATE irc_servers SET status='connected', last_connected_at=datetime('now'), retry_count=0, updated_at=datetime('now') WHERE id=?`,
		id,
	)
	return err
}

func (s *SQLiteStore) IncrementServerRetry(id int64) error {
	_, err := s.db.Exec(
		`UPDATE irc_servers SET retry_count=retry_count+1, status='reconnecting', updated_at=datetime('now') WHERE id=?`,
		id,
	)
	return err
}

// =========================================================================
// IRC Channels
// =========================================================================

func (s *SQLiteStore) AddChannel(ch ChannelRecord) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO irc_channels (server_id, name, topic, auto_join, joined)
		 VALUES (?, ?, ?, ?, ?)`,
		ch.ServerID, ch.Name, ch.Topic, boolToInt(ch.AutoJoin), boolToInt(ch.Joined),
	)
	if err != nil {
		return 0, fmt.Errorf("adding channel: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetChannelsByServer(serverID int64) ([]ChannelRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, server_id, name, topic, auto_join, joined
		 FROM irc_channels WHERE server_id = ? ORDER BY name`, serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting channels for server %d: %w", serverID, err)
	}
	defer rows.Close()

	var channels []ChannelRecord
	for rows.Next() {
		var ch ChannelRecord
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Topic, &ch.AutoJoin, &ch.Joined); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if channels == nil {
		channels = []ChannelRecord{}
	}
	return channels, rows.Err()
}

func (s *SQLiteStore) GetChannelsByServerAndName(serverID int64, name string) (*ChannelRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, server_id, name, topic, auto_join, joined
		 FROM irc_channels WHERE server_id = ? AND name = ?`, serverID, name,
	)
	var ch ChannelRecord
	if err := row.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Topic, &ch.AutoJoin, &ch.Joined); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning channel: %w", err)
	}
	return &ch, nil
}

func (s *SQLiteStore) UpdateChannel(ch ChannelRecord) error {
	_, err := s.db.Exec(
		`UPDATE irc_channels SET name=?, topic=?, auto_join=?, joined=? WHERE id=?`,
		ch.Name, ch.Topic, boolToInt(ch.AutoJoin), boolToInt(ch.Joined), ch.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteChannel(id int64) error {
	_, err := s.db.Exec(`DELETE FROM irc_channels WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) SetChannelJoined(id int64, joined bool) error {
	_, err := s.db.Exec(`UPDATE irc_channels SET joined=? WHERE id=?`, boolToInt(joined), id)
	return err
}

func (s *SQLiteStore) UpdateChannelTopic(id int64, topic string) error {
	_, err := s.db.Exec(`UPDATE irc_channels SET topic=? WHERE id=?`, topic, id)
	return err
}

func (s *SQLiteStore) GetAutoJoinChannels() ([]ChannelRecord, error) {
	rows, err := s.db.Query(
		`SELECT ch.id, ch.server_id, ch.name, ch.topic, ch.auto_join, ch.joined
		 FROM irc_channels ch
		 JOIN irc_servers srv ON srv.id = ch.server_id
		 WHERE ch.auto_join = 1 AND srv.auto_connect = 1
		 ORDER BY ch.server_id, ch.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("getting auto-join channels: %w", err)
	}
	defer rows.Close()

	var channels []ChannelRecord
	for rows.Next() {
		var ch ChannelRecord
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Topic, &ch.AutoJoin, &ch.Joined); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if channels == nil {
		channels = []ChannelRecord{}
	}
	return channels, rows.Err()
}

// =========================================================================
// Downloads
// =========================================================================

func (s *SQLiteStore) EnqueueDownload(d DownloadRecord) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO downloads (pack_message, bot, server_address, channel, filename, file_size, status, priority, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, datetime('now'))`,
		d.PackMessage, d.Bot, d.ServerAddress, d.Channel, d.Filename, d.FileSize, d.Priority,
	)
	if err != nil {
		return 0, fmt.Errorf("enqueueing download: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetDownload(id int64) (*DownloadRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE id = ?`, id,
	)
	return scanDownload(row)
}

func (s *SQLiteStore) GetQueue() ([]DownloadRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE status IN ('queued', 'downloading', 'paused')
		 ORDER BY priority ASC, created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("getting queue: %w", err)
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *SQLiteStore) GetQueueByChannel(channel string) ([]DownloadRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE channel = ? AND status IN ('queued', 'downloading', 'paused')
		 ORDER BY priority ASC, created_at ASC`, channel,
	)
	if err != nil {
		return nil, fmt.Errorf("getting queue for channel %s: %w", channel, err)
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *SQLiteStore) GetActiveDownloads() ([]DownloadRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE status = 'downloading'
		 ORDER BY priority ASC, created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("getting active downloads: %w", err)
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *SQLiteStore) GetPendingByChannel(channel string) ([]DownloadRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE channel = ? AND status = 'queued'
		 ORDER BY priority ASC, created_at ASC`, channel,
	)
	if err != nil {
		return nil, fmt.Errorf("getting pending downloads for channel %s: %w", channel, err)
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *SQLiteStore) UpdateDownloadProgress(id int64, progressBytes int64, speedBPS int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET progress_bytes=?, speed_bps=?, status='downloading' WHERE id=?`,
		progressBytes, speedBPS, id,
	)
	return err
}

func (s *SQLiteStore) MarkDownloadStarted(id int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='downloading', started_at=datetime('now') WHERE id=?`, id,
	)
	return err
}

func (s *SQLiteStore) MarkDownloadCompleted(id int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='completed', completed_at=datetime('now'), progress_bytes=file_size WHERE id=?`, id,
	)
	return err
}

func (s *SQLiteStore) MarkDownloadSkipped(id int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='skipped_existing' WHERE id=? AND status IN ('downloading','queued')`,
		id,
	)
	return err
}

func (s *SQLiteStore) MarkDownloadFailed(id int64, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='failed', error_message=?, completed_at=datetime('now') WHERE id=?`,
		errMsg, id,
	)
	return err
}

func (s *SQLiteStore) MarkDownloadPaused(id int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='paused' WHERE id=? AND status IN ('queued', 'downloading')`, id,
	)
	return err
}

func (s *SQLiteStore) MarkDownloadRetry(id int64, newStatus string) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status=?, error_message='' WHERE id=?`, newStatus, id,
	)
	return err
}

func (s *SQLiteStore) DeleteDownload(id int64) error {
	_, err := s.db.Exec(`DELETE FROM downloads WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) RetryDownload(id int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='queued', progress_bytes=0, error_message='', completed_at=NULL WHERE id=? AND status IN ('failed', 'paused', 'completed', 'skipped_existing')`,
		id,
	)
	return err
}

func (s *SQLiteStore) RequeueDownload(id int64) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET status='queued', progress_bytes=0, error_message='' WHERE id=?`,
		id,
	)
	return err
}

func (s *SQLiteStore) SetDownloadPriority(id int64, priority int) error {
	_, err := s.db.Exec(`UPDATE downloads SET priority=? WHERE id=?`, priority, id)
	return err
}

func (s *SQLiteStore) GetTotalDownloadedBytes() (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRow(
		`SELECT SUM(progress_bytes) FROM downloads`,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("getting total downloaded bytes: %w", err)
	}
	if total.Valid {
		return total.Int64, nil
	}
	return 0, nil
}

func (s *SQLiteStore) GetDownloadHistory(page, pageSize int) ([]DownloadRecord, int, error) {
	// Count total
	var total int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM downloads WHERE status IN ('completed', 'failed', 'skipped_existing')`,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting download history: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := s.db.Query(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE status IN ('completed', 'failed', 'skipped_existing')
		 ORDER BY completed_at DESC, created_at DESC
		 LIMIT ? OFFSET ?`, pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("getting download history: %w", err)
	}
	defer rows.Close()

	downloads, err := scanDownloads(rows)
	if err != nil {
		return nil, 0, err
	}
	return downloads, total, nil
}

func (s *SQLiteStore) BulkActionDownloads(ids []int64, action string) (map[int64]string, error) {
	results := make(map[int64]string)
	for _, id := range ids {
		var err error
		switch strings.ToLower(action) {
		case "pause":
			err = s.MarkDownloadPaused(id)
		case "resume":
			err = s.RetryDownload(id)
		case "remove":
			err = s.DeleteDownload(id)
		default:
			results[id] = fmt.Sprintf("unknown action: %s", action)
			continue
		}
		if err != nil {
			results[id] = err.Error()
		} else {
			results[id] = "success"
		}
	}
	return results, nil
}

func (s *SQLiteStore) FindDuplicateDownload(bot, serverAddress string, packNumber int) (*DownloadRecord, error) {
	// Use exact pack message match to avoid LIKE matching wrong pack numbers
	// (e.g. '#1' matching '#10', '#11', etc.). Match both the full message and
	// messages that end with the exact pack reference (e.g. 'xdcc send #42').
	packExact := fmt.Sprintf("xdcc send #%d", packNumber)
	row := s.db.QueryRow(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads
		 WHERE bot = ? AND server_address = ? AND (pack_message = ? OR pack_message = ?)
		 ORDER BY created_at DESC LIMIT 1`,
		bot, serverAddress, packExact, "/msg "+bot+" "+packExact,
	)
	return scanDownload(row)
}

func (s *SQLiteStore) GetDownloadByBotMessage(bot, packMessage string) (*DownloadRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE bot = ? AND pack_message = ?
		 ORDER BY created_at DESC LIMIT 1`,
		bot, packMessage,
	)
	return scanDownload(row)
}

// =========================================================================
// Search Cache
// =========================================================================

func (s *SQLiteStore) SetSearchCache(entry SearchCacheEntry) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO search_cache (query_key, provider, payload_json, fetched_at, expires_at, stale_expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.QueryKey, entry.Provider, entry.PayloadJSON,
		entry.FetchedAt.Format(time.RFC3339),
		entry.ExpiresAt.Format(time.RFC3339),
		entry.StaleExpiresAt.Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetSearchCache(queryKey, provider string) (*SearchCacheEntry, error) {
	row := s.db.QueryRow(
		`SELECT query_key, provider, payload_json, fetched_at, expires_at, stale_expires_at
		 FROM search_cache WHERE query_key = ? AND provider = ?`, queryKey, provider,
	)
	var entry SearchCacheEntry
	var fetchedAt, expiresAt, staleExpiresAt string
	if err := row.Scan(&entry.QueryKey, &entry.Provider, &entry.PayloadJSON,
		&fetchedAt, &expiresAt, &staleExpiresAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning search cache: %w", err)
	}
	var err error
	entry.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return nil, err
	}
	entry.ExpiresAt, err = time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return nil, err
	}
	entry.StaleExpiresAt, err = time.Parse(time.RFC3339, staleExpiresAt)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetSearchCacheByQuery returns all cache entries for a given query key in a single query.
// This avoids the need for nested queries which can deadlock on single-connection SQLite.
func (s *SQLiteStore) GetSearchCacheByQuery(queryKey string) ([]SearchCacheEntry, error) {
	rows, err := s.db.Query(
		`SELECT query_key, provider, payload_json, fetched_at, expires_at, stale_expires_at
		 FROM search_cache WHERE query_key = ?`, queryKey,
	)
	if err != nil {
		return nil, fmt.Errorf("querying search cache by query: %w", err)
	}
	defer rows.Close()

	var entries []SearchCacheEntry
	for rows.Next() {
		var entry SearchCacheEntry
		var fetchedAt, expiresAt, staleExpiresAt string
		if err := rows.Scan(&entry.QueryKey, &entry.Provider, &entry.PayloadJSON,
			&fetchedAt, &expiresAt, &staleExpiresAt); err != nil {
			return nil, fmt.Errorf("scanning search cache row: %w", err)
		}
		entry.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
		if err != nil {
			continue
		}
		entry.ExpiresAt, err = time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			continue
		}
		entry.StaleExpiresAt, err = time.Parse(time.RFC3339, staleExpiresAt)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) DeleteExpiredSearchCache(staleBefore time.Time) error {
	_, err := s.db.Exec(
		`DELETE FROM search_cache WHERE stale_expires_at < ?`,
		staleBefore.Format(time.RFC3339),
	)
	return err
}

// CleanupSearchCache removes stale cache entries beyond their stale TTL.
// Returns the number of entries deleted.
func (s *SQLiteStore) CleanupSearchCache() (int, error) {
	now := time.Now()
	result, err := s.db.Exec(
		`DELETE FROM search_cache WHERE stale_expires_at < ?`,
		now.Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("cleaning up search cache: %w", err)
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// =========================================================================
// Search Presets
// =========================================================================

func (s *SQLiteStore) AddSearchPreset(p SearchPreset) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO search_presets (name, query, filters_json, is_default)
		 VALUES (?, ?, ?, ?)`,
		p.Name, p.Query, p.FiltersJSON, boolToInt(p.IsDefault),
	)
	if err != nil {
		return 0, fmt.Errorf("adding search preset: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetSearchPreset(id int64) (*SearchPreset, error) {
	row := s.db.QueryRow(
		`SELECT id, name, query, filters_json, is_default, created_at, updated_at
		 FROM search_presets WHERE id = ?`, id,
	)
	return scanSearchPreset(row)
}

func (s *SQLiteStore) ListSearchPresets() ([]SearchPreset, error) {
	rows, err := s.db.Query(
		`SELECT id, name, query, filters_json, is_default, created_at, updated_at
		 FROM search_presets ORDER BY is_default DESC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing search presets: %w", err)
	}
	defer rows.Close()

	var presets []SearchPreset
	for rows.Next() {
		p, err := scanSearchPresetFromRows(rows)
		if err != nil {
			return nil, err
		}
		presets = append(presets, *p)
	}
	if presets == nil {
		presets = []SearchPreset{}
	}
	return presets, rows.Err()
}

func (s *SQLiteStore) UpdateSearchPreset(p SearchPreset) error {
	_, err := s.db.Exec(
		`UPDATE search_presets SET name=?, query=?, filters_json=?, is_default=?, updated_at=datetime('now') WHERE id=?`,
		p.Name, p.Query, p.FiltersJSON, boolToInt(p.IsDefault), p.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteSearchPreset(id int64) error {
	_, err := s.db.Exec(`DELETE FROM search_presets WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) SetDefaultSearchPreset(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear all defaults
	if _, err := tx.Exec(`UPDATE search_presets SET is_default=0`); err != nil {
		return err
	}
	// Set new default
	if _, err := tx.Exec(`UPDATE search_presets SET is_default=1 WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// =========================================================================
// Watchlists
// =========================================================================

func (s *SQLiteStore) AddWatchlist(w Watchlist) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO watchlists (name, query, filters_json, enabled, auto_enqueue)
		 VALUES (?, ?, ?, ?, ?)`,
		w.Name, w.Query, w.FiltersJSON, boolToInt(w.Enabled), boolToInt(w.AutoEnqueue),
	)
	if err != nil {
		return 0, fmt.Errorf("adding watchlist: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetWatchlist(id int64) (*Watchlist, error) {
	row := s.db.QueryRow(
		`SELECT id, name, query, filters_json, enabled, auto_enqueue,
		        last_checked_at, last_match_fingerprint, last_notified_at,
		        created_at, updated_at
		 FROM watchlists WHERE id = ?`, id,
	)
	return scanWatchlist(row)
}

func (s *SQLiteStore) ListWatchlists() ([]Watchlist, error) {
	rows, err := s.db.Query(
		`SELECT id, name, query, filters_json, enabled, auto_enqueue,
		        last_checked_at, last_match_fingerprint, last_notified_at,
		        created_at, updated_at
		 FROM watchlists ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing watchlists: %w", err)
	}
	defer rows.Close()

	var watchlists []Watchlist
	for rows.Next() {
		w, err := scanWatchlistFromRows(rows)
		if err != nil {
			return nil, err
		}
		watchlists = append(watchlists, *w)
	}
	if watchlists == nil {
		watchlists = []Watchlist{}
	}
	return watchlists, rows.Err()
}

func (s *SQLiteStore) UpdateWatchlist(w Watchlist) error {
	_, err := s.db.Exec(
		`UPDATE watchlists SET name=?, query=?, filters_json=?, enabled=?, auto_enqueue=?, updated_at=datetime('now') WHERE id=?`,
		w.Name, w.Query, w.FiltersJSON, boolToInt(w.Enabled), boolToInt(w.AutoEnqueue), w.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteWatchlist(id int64) error {
	_, err := s.db.Exec(`DELETE FROM watchlists WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) SetWatchlistChecked(id int64, fingerprint string) error {
	_, err := s.db.Exec(
		`UPDATE watchlists SET last_checked_at=datetime('now'), last_match_fingerprint=? WHERE id=?`,
		fingerprint, id,
	)
	return err
}

func (s *SQLiteStore) SetWatchlistNotified(id int64) error {
	_, err := s.db.Exec(
		`UPDATE watchlists SET last_notified_at=datetime('now') WHERE id=?`, id,
	)
	return err
}

func (s *SQLiteStore) GetEnabledWatchlists() ([]Watchlist, error) {
	rows, err := s.db.Query(
		`SELECT id, name, query, filters_json, enabled, auto_enqueue,
		        last_checked_at, last_match_fingerprint, last_notified_at,
		        created_at, updated_at
		 FROM watchlists WHERE enabled = 1 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("getting enabled watchlists: %w", err)
	}
	defer rows.Close()

	var watchlists []Watchlist
	for rows.Next() {
		w, err := scanWatchlistFromRows(rows)
		if err != nil {
			return nil, err
		}
		watchlists = append(watchlists, *w)
	}
	if watchlists == nil {
		watchlists = []Watchlist{}
	}
	return watchlists, rows.Err()
}

// =========================================================================
// Provider Stats
// =========================================================================

func (s *SQLiteStore) RecordProviderStats(stats ProviderStats) error {
	_, err := s.db.Exec(
		`INSERT INTO provider_stats (provider, window_start, window_end, requests, successes, timeouts, failures, avg_latency_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		stats.Provider,
		stats.WindowStart.Format(time.RFC3339),
		stats.WindowEnd.Format(time.RFC3339),
		stats.Requests, stats.Successes, stats.Timeouts,
		stats.Failures, stats.AvgLatencyMs,
	)
	return err
}

func (s *SQLiteStore) GetProviderStats(provider string, since time.Time) ([]ProviderStats, error) {
	rows, err := s.db.Query(
		`SELECT provider, window_start, window_end, requests, successes, timeouts, failures, avg_latency_ms, updated_at
		 FROM provider_stats WHERE provider = ? AND window_start >= ?
		 ORDER BY window_start DESC`, provider, since.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("getting provider stats: %w", err)
	}
	defer rows.Close()

	var stats []ProviderStats
	for rows.Next() {
		var st ProviderStats
		var ws, we, updated string
		if err := rows.Scan(&st.Provider, &ws, &we, &st.Requests, &st.Successes,
			&st.Timeouts, &st.Failures, &st.AvgLatencyMs, &updated); err != nil {
			return nil, fmt.Errorf("scanning provider stats: %w", err)
		}
		st.WindowStart, _ = time.Parse(time.RFC3339, ws)
		st.WindowEnd, _ = time.Parse(time.RFC3339, we)
		st.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		stats = append(stats, st)
	}
	if stats == nil {
		stats = []ProviderStats{}
	}
	return stats, rows.Err()
}

func (s *SQLiteStore) GetAllProviderStats(since time.Time) (map[string][]ProviderStats, error) {
	rows, err := s.db.Query(
		`SELECT provider, window_start, window_end, requests, successes, timeouts, failures, avg_latency_ms, updated_at
		 FROM provider_stats WHERE window_start >= ?
		 ORDER BY provider, window_start DESC`, since.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("getting all provider stats: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]ProviderStats)
	for rows.Next() {
		var st ProviderStats
		var ws, we, updated string
		if err := rows.Scan(&st.Provider, &ws, &we, &st.Requests, &st.Successes,
			&st.Timeouts, &st.Failures, &st.AvgLatencyMs, &updated); err != nil {
			return nil, fmt.Errorf("scanning provider stats: %w", err)
		}
		st.WindowStart, _ = time.Parse(time.RFC3339, ws)
		st.WindowEnd, _ = time.Parse(time.RFC3339, we)
		st.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		result[st.Provider] = append(result[st.Provider], st)
	}
	return result, rows.Err()
}

// =========================================================================
// Scan helpers
// =========================================================================

func scanServer(row interface{ Scan(...any) error }) (*ServerRecord, error) {
	var srv ServerRecord
	var lastConnected, createdAtStr, updatedAtStr sql.NullString
	var autoConnect int
	err := row.Scan(&srv.ID, &srv.Address, &srv.Port, &autoConnect,
		&srv.Status, &lastConnected, &srv.RetryCount, &createdAtStr, &updatedAtStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning server: %w", err)
	}
	srv.AutoConnect = autoConnect != 0
	if lastConnected.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", lastConnected.String)
		if err == nil {
			srv.LastConnectedAt = &t
		}
	}
	if createdAtStr.Valid {
		srv.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr.String)
	}
	if updatedAtStr.Valid {
		srv.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAtStr.String)
	}
	return &srv, nil
}

func scanDownload(row interface{ Scan(...any) error }) (*DownloadRecord, error) {
	var d DownloadRecord
	var startedAt, completedAt, createdAt sql.NullString
	err := row.Scan(&d.ID, &d.PackMessage, &d.Bot, &d.ServerAddress, &d.Channel,
		&d.Filename, &d.FileSize, &d.Status, &d.ProgressBytes, &d.SpeedBPS,
		&d.ErrorMessage, &d.Priority, &createdAt, &startedAt, &completedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning download: %w", err)
	}
	if createdAt.Valid {
		d.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt.String)
	}
	if startedAt.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", startedAt.String)
		if err == nil {
			d.StartedAt = &t
		}
	}
	if completedAt.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", completedAt.String)
		if err == nil {
			d.CompletedAt = &t
		}
	}
	return &d, nil
}

func scanDownloads(rows *sql.Rows) ([]DownloadRecord, error) {
	var downloads []DownloadRecord
	for rows.Next() {
		d, err := scanDownloadFromRows(rows)
		if err != nil {
			return nil, err
		}
		downloads = append(downloads, *d)
	}
	if downloads == nil {
		downloads = []DownloadRecord{}
	}
	return downloads, rows.Err()
}

func scanDownloadFromRows(rows *sql.Rows) (*DownloadRecord, error) {
	var d DownloadRecord
	var startedAt, completedAt, createdAt sql.NullString
	if err := rows.Scan(&d.ID, &d.PackMessage, &d.Bot, &d.ServerAddress, &d.Channel,
		&d.Filename, &d.FileSize, &d.Status, &d.ProgressBytes, &d.SpeedBPS,
		&d.ErrorMessage, &d.Priority, &createdAt, &startedAt, &completedAt); err != nil {
		return nil, fmt.Errorf("scanning download: %w", err)
	}
	if createdAt.Valid {
		d.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt.String)
	}
	if startedAt.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", startedAt.String)
		if err == nil {
			d.StartedAt = &t
		}
	}
	if completedAt.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", completedAt.String)
		if err == nil {
			d.CompletedAt = &t
		}
	}
	return &d, nil
}

// =========================================================================
// Scan helpers for SearchPreset
// =========================================================================

func scanSearchPreset(row interface{ Scan(...any) error }) (*SearchPreset, error) {
	var p SearchPreset
	var createdAt, updatedAt sql.NullString
	err := row.Scan(&p.ID, &p.Name, &p.Query, &p.FiltersJSON, &p.IsDefault, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning search preset: %w", err)
	}
	if createdAt.Valid {
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt.String)
	}
	if updatedAt.Valid {
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt.String)
	}
	return &p, nil
}

func scanSearchPresetFromRows(rows *sql.Rows) (*SearchPreset, error) {
	var p SearchPreset
	var createdAt, updatedAt sql.NullString
	if err := rows.Scan(&p.ID, &p.Name, &p.Query, &p.FiltersJSON, &p.IsDefault, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("scanning search preset: %w", err)
	}
	if createdAt.Valid {
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt.String)
	}
	if updatedAt.Valid {
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt.String)
	}
	return &p, nil
}

func scanWatchlist(row interface{ Scan(...any) error }) (*Watchlist, error) {
	var w Watchlist
	var lastChecked, lastNotified, createdAtStr, updatedAtStr sql.NullString
	err := row.Scan(&w.ID, &w.Name, &w.Query, &w.FiltersJSON, &w.Enabled,
		&w.AutoEnqueue, &lastChecked, &w.LastMatchFingerprint, &lastNotified,
		&createdAtStr, &updatedAtStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning watchlist: %w", err)
	}
	if createdAtStr.Valid {
		w.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr.String)
	}
	if updatedAtStr.Valid {
		w.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAtStr.String)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning watchlist: %w", err)
	}
	if lastChecked.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", lastChecked.String)
		if err == nil {
			w.LastCheckedAt = &t
		}
	}
	if lastNotified.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", lastNotified.String)
		if err == nil {
			w.LastNotifiedAt = &t
		}
	}
	return &w, nil
}

func scanWatchlistFromRows(rows interface{ Scan(...any) error }) (*Watchlist, error) {
	var w Watchlist
	var lastChecked, lastNotified, createdAtStr, updatedAtStr sql.NullString
	if err := rows.Scan(&w.ID, &w.Name, &w.Query, &w.FiltersJSON, &w.Enabled,
		&w.AutoEnqueue, &lastChecked, &w.LastMatchFingerprint, &lastNotified,
		&createdAtStr, &updatedAtStr); err != nil {
		return nil, fmt.Errorf("scanning watchlist: %w", err)
	}
	if lastChecked.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", lastChecked.String)
		if err == nil {
			w.LastCheckedAt = &t
		}
	}
	if lastNotified.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", lastNotified.String)
		if err == nil {
			w.LastNotifiedAt = &t
		}
	}
	if createdAtStr.Valid {
		w.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr.String)
	}
	if updatedAtStr.Valid {
		w.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAtStr.String)
	}
	return &w, nil
}

// =========================================================================
// Helpers
// =========================================================================

// boolToInt converts a bool to 0 or 1 for SQLite integer storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
