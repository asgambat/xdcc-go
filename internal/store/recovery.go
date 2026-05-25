package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ---------------------------------------------------------------------------
// Recovery — 2.4: Recover downloads on startup
// ---------------------------------------------------------------------------

// RecoverOnStartup implements the recovery logic:
//   - Downloads with status 'downloading' are requeued as 'queued'
//   - Returns the list of affected downloads for the caller to act on
func (s *SQLiteStore) RecoverDownloadsOnStartup() ([]DownloadRecord, error) {
	// Find all downloads stuck in 'downloading' status
	rows, err := s.db.Query(
		`SELECT id, pack_message, bot, server_address, channel, filename, file_size,
		        status, progress_bytes, speed_bps, error_message, priority,
		        created_at, started_at, completed_at
		 FROM downloads WHERE status = 'downloading'`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying downloading records for recovery: %w", err)
	}
	defer rows.Close()

	stuck, err := s.scanDownloads(rows)
	if err != nil {
		return nil, err
	}

	if len(stuck) == 0 {
		return stuck, nil
	}

	// Requeue them
	ids := make([]int64, 0, len(stuck))
	for _, d := range stuck {
		ids = append(ids, d.ID)
	}

	if err := s.batchRequeue(ids); err != nil {
		return nil, fmt.Errorf("requeueing %d downloads: %w", len(ids), err)
	}

	return stuck, nil
}

// batchRequeue sets multiple downloads back to 'queued' with progress reset.
func (s *SQLiteStore) batchRequeue(ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // read-only in practice; rollback failure is harmless

	stmt, err := tx.Prepare(
		`UPDATE downloads SET status='queued', progress_bytes=0, error_message='', speed_bps=0 WHERE id=?`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.Exec(id); err != nil {
			return fmt.Errorf("requeueing download %d: %w", id, err)
		}
	}

	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Reconciliation — 2.7: DB <-> filesystem reconciliation
// ---------------------------------------------------------------------------

// ReconcileFileSystem checks partial files on disk against database records,
// and cleans up orphaned temporary files.
//
// Parameters:
//   - tempDir: directory where partial downloads are stored
//   - orphanedPolicy: "delete" or "move" for orphaned temp files
//   - orphanedDir: directory to move orphaned files to (if policy is "move")
func (s *SQLiteStore) ReconcileFileSystem(tempDir, orphanedPolicy, orphanedDir string) ([]string, error) {
	var actions []string

	// 1. Check 'downloading' records whose temp files are missing → requeue them
	downloading, err := s.GetActiveDownloads()
	if err != nil {
		return nil, fmt.Errorf("getting active downloads for reconciliation: %w", err)
	}

	for _, d := range downloading {
		tempPath := filepath.Join(tempDir, d.Filename)
		if _, err := os.Stat(tempPath); os.IsNotExist(err) {
			// Temp file is missing, requeue
			if err := s.RequeueDownload(d.ID); err != nil {
				actions = append(actions, fmt.Sprintf("ERROR requeueing download %d: %v", d.ID, err))
			} else {
				actions = append(actions, fmt.Sprintf("REQUEUED download %d (temp file missing: %s)", d.ID, tempPath))
			}
		}
	}

	// 2. Find orphaned temp files (files in tempDir not associated with any active/queued download)
	// Build a set of filenames from active/paused/queued downloads
	queue, err := s.GetQueue()
	if err != nil {
		return nil, fmt.Errorf("getting queue for reconciliation: %w", err)
	}

	activeFiles := make(map[string]bool)
	for _, d := range queue {
		activeFiles[d.Filename] = true
	}

	// Read temp directory
	entries, err := os.ReadDir(tempDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading temp dir %s: %w", tempDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !activeFiles[entry.Name()] {
			orphanedPath := filepath.Join(tempDir, entry.Name())
			switch orphanedPolicy {
			case "delete":
				if err := os.Remove(orphanedPath); err != nil {
					actions = append(actions, fmt.Sprintf("ERROR deleting orphaned file %s: %v", orphanedPath, err))
				} else {
					actions = append(actions, fmt.Sprintf("DELETED orphaned temp file: %s", orphanedPath))
				}
			case "move":
				if orphanedDir == "" {
					orphanedDir = filepath.Join(tempDir, "orphaned")
				}
				if err := os.MkdirAll(orphanedDir, 0o755); err != nil {
					actions = append(actions, fmt.Sprintf("ERROR creating orphaned dir %s: %v", orphanedDir, err))
					continue
				}
				dest := filepath.Join(orphanedDir, entry.Name())
				if err := os.Rename(orphanedPath, dest); err != nil {
					actions = append(actions, fmt.Sprintf("ERROR moving orphaned file %s: %v", orphanedPath, err))
				} else {
					actions = append(actions, fmt.Sprintf("MOVED orphaned temp file: %s → %s", orphanedPath, dest))
				}
			default:
				// skip — leave orphaned files in place
				actions = append(actions, fmt.Sprintf("SKIPPED orphaned temp file: %s", orphanedPath))
			}
		}
	}

	return actions, nil
}

// ---------------------------------------------------------------------------
// Vacuum
// ---------------------------------------------------------------------------

// Vacuum reclaims disk space by running SQLite VACUUM.
func (s *SQLiteStore) Vacuum() error {
	_, err := s.db.Exec("VACUUM")
	if err != nil {
		return fmt.Errorf("running VACUUM: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cleanup — 2.5: Periodic cleanup
// ---------------------------------------------------------------------------

// CleanupOldDownloads deletes completed/failed downloads older than retentionDays.
// Returns the number of deleted records.
func (s *SQLiteStore) CleanupOldDownloads(retentionDays int) (int, error) {
	result, err := s.db.Exec(
		`DELETE FROM downloads
		 WHERE status IN ('completed', 'failed', 'skipped_existing')
		   AND completed_at IS NOT NULL
		   AND completed_at < datetime('now', ?)`,
		fmt.Sprintf("-%d days", retentionDays),
	)
	if err != nil {
		return 0, fmt.Errorf("cleaning up old downloads: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// RunCleanup starts a background goroutine that periodically:
// 1. Deletes old completed/failed downloads
// 2. Removes expired search cache entries
// 3. Runs VACUUM once a week
//
// Returns two channels:
// - stopCh: close this to signal the goroutine to stop
// - doneCh: closed when the goroutine has completely stopped
func (s *SQLiteStore) RunCleanup(retentionDays int, cleanupInterval time.Duration) (stopCh, doneCh chan struct{}, err error) {
	stopCh = make(chan struct{})
	doneCh = make(chan struct{})

	go func() {
		defer close(doneCh)

		// Track when VACUUM was last run
		lastVacuum := time.Now()

		for {
			select {
			case <-stopCh:
				return
			case <-time.After(cleanupInterval):
				// Cleanup old downloads
				_, _ = s.CleanupOldDownloads(retentionDays)

				// Cleanup expired search cache (entries older than stale TTL)
				_ = s.DeleteExpiredSearchCache(time.Now().Add(-24 * time.Hour))

				// VACUUM once a week
				if time.Since(lastVacuum) >= 7*24*time.Hour {
					if err := s.Vacuum(); err == nil {
						lastVacuum = time.Now()
					}
				}
			}
		}
	}()

	return stopCh, doneCh, nil
}
