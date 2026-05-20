package queue

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"xdcc-go/internal/config"
	"xdcc-go/internal/diskmon"
	xdccirc "xdcc-go/internal/irc"
	"xdcc-go/internal/store"
)

// normalizeChannel lowercases and ensures a leading '#'.
func normalizeChannel(ch string) string {
	ch = strings.ToLower(strings.TrimSpace(ch))
	if ch != "" && !strings.HasPrefix(ch, "#") {
		ch = "#" + ch
	}
	return ch
}

// ---------------------------------------------------------------------------
// QueueManager
// ---------------------------------------------------------------------------

// QueueManager manages the download queue. It enforces:
//   - Max 1 active download per IRC channel
//   - A global parallel download limit (default 5)
//   - FIFO priority per-channel queue
//   - Persistence via SQLite store
//   - Real-time events for SSE propagation
type QueueManager struct {
	store store.Store
	cfg   *config.Config
	log   *log.Logger

	mu sync.RWMutex
	// activeJobs tracks currently running downloads: download ID → cancel function
	activeJobs map[int64]context.CancelFunc
	// channelSlots tracks which channels currently have an active download
	channelSlots map[string]int64 // channel (normalized) → download ID
	// globalCount is the number of currently active downloads
	globalCount int

	// event subscriber hub
	subscriber *subscriberHub

	// main context for lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// Track active download goroutines for clean shutdown
	downloadWg sync.WaitGroup

	// Disk monitor for available space checks
	diskMon       *diskmon.Monitor
	diskLow       bool
	stopDiskCheck func()
	diskCheckDone <-chan struct{}
}

// New creates a new QueueManager.
func New(st store.Store, cfg *config.Config, logger *log.Logger) *QueueManager {
	ctx, cancel := context.WithCancel(context.Background())
	qm := &QueueManager{
		store:        st,
		cfg:          cfg,
		log:          logger,
		activeJobs:   make(map[int64]context.CancelFunc),
		channelSlots: make(map[string]int64),
		subscriber:   newSubscriberHub(),
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
	}

	// Initialize disk monitor if threshold > 0
	if cfg.Download.MinDiskSpace > 0 {
		qm.diskMon = diskmon.New(cfg.Download.TempDir, cfg.Download.MinDiskSpace, nil, logger)
		// Start periodic check — auto-resume when space recovers
		qm.stopDiskCheck, qm.diskCheckDone = qm.diskMon.StartPeriodicCheck(func(low bool, _ int64) {
			qm.mu.Lock()
			qm.diskLow = low
			qm.mu.Unlock()
			if low {
				logger.Printf("DISK LOW: queue paused until disk space recovers")
				qm.emitEvent(Event{
					Type: EventDiskSpaceLow,
				})
			} else {
				logger.Printf("DISK OK: space recovered, resuming queue")
				qm.emitEvent(Event{
					Type: EventDiskSpaceOK,
				})
				qm.tryDispatch()
			}
		})
	}

	return qm
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start begins the periodic monitor goroutine. It should be called after
// the store has been initialized and migrations have run.
//
// On startup, it recovers downloads that were in 'downloading' status
// (re-queued by the store) and tries to dispatch them.
func (qm *QueueManager) Start() error {
	// Recovery is handled by the caller (main.go) before creating the queue
	// manager. We just need to dispatch any queued items and start the monitor.
	qm.tryDispatch()

	// Start the periodic monitor goroutine
	go qm.monitorLoop()

	return nil
}

// Stop cancels all active downloads and stops the monitor.
func (qm *QueueManager) Stop() {
	// Stop disk monitor first and wait for goroutine to exit
	if qm.stopDiskCheck != nil {
		qm.stopDiskCheck()
		<-qm.diskCheckDone
	}

	qm.cancel()
	<-qm.done

	// Save progress of all active downloads before cancelling
	qm.mu.RLock()
	ids := make([]int64, 0, len(qm.activeJobs))
	for id := range qm.activeJobs {
		ids = append(ids, id)
	}
	qm.mu.RUnlock()

	for _, id := range ids {
		// Save progress before cancellation
		if d, err := qm.store.GetDownload(id); err == nil && d != nil && d.ProgressBytes > 0 {
			qm.log.Printf("shutdown: saving progress for download %d: %d/%d bytes", id, d.ProgressBytes, d.FileSize)
		}
		qm.CancelDownload(id, "server shutting down")
	}

	// Wait for all download workers to complete with timeout
	downloadsDone := make(chan struct{})
	go func() {
		qm.downloadWg.Wait()
		close(downloadsDone)
	}()

	select {
	case <-downloadsDone:
		qm.log.Printf("all download workers stopped cleanly")
	case <-time.After(10 * time.Second):
		qm.log.Printf("WARNING: download workers did not stop within 10s")
	}
}

// monitorLoop periodically checks for queued downloads that can be started.
// It runs every 10 seconds by default.
func (qm *QueueManager) monitorLoop() {
	defer close(qm.done)

	// Dispatch interval: 10 seconds by default
	// (configurable via dedicated config field in a future update)
	interval := 10 * time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-qm.ctx.Done():
			return
		case <-ticker.C:
			qm.tryDispatch()
		}
	}
}

// ---------------------------------------------------------------------------
// Subscribe / Unsubscribe (for SSE propagation)
// ---------------------------------------------------------------------------

// Subscribe returns a channel that receives queue events.
func (qm *QueueManager) Subscribe() chan Event {
	return qm.subscriber.subscribe()
}

// Unsubscribe removes a previously subscribed channel.
func (qm *QueueManager) Unsubscribe(ch chan Event) {
	qm.subscriber.unsubscribe(ch)
}

// emitEvent sends an event to all subscribers.
func (qm *QueueManager) emitEvent(evt Event) {
	evt.Timestamp = time.Now()
	qm.subscriber.publish(evt)
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Enqueue adds a download to the queue. It persists the record first, then
// tries to dispatch immediately.
//
// packMessage is the raw XDCC message (e.g. "xdcc send #123").
// The caller should already have validated it.
func (qm *QueueManager) Enqueue(d store.DownloadRecord) (int64, error) {
	// Normalize and validate channel
	d.Channel = normalizeChannel(d.Channel)
	if d.Channel == "" {
		return 0, fmt.Errorf("channel name is required")
	}
	
	// Check for duplicate by bot + pack message
	dupByMsg, err := qm.store.GetDownloadByBotMessage(d.Bot, d.PackMessage)
	if err == nil && dupByMsg != nil && dupByMsg.Status != store.DownloadStatusCompleted {
		return 0, fmt.Errorf("duplicate download: already %s (id=%d)", dupByMsg.Status, dupByMsg.ID)
	}

	// Check disk space before enqueuing
	if qm.diskMon != nil {
		_, _, low, err := qm.diskMon.Check()
		if err == nil && low {
			return 0, fmt.Errorf("insufficient disk space: %s available, need %s",
				diskmon.FormatBytes(qm.cfg.Download.MinDiskSpace),
				diskmon.FormatBytes(qm.cfg.Download.MinDiskSpace))
		}
	}

	// Set default priority
	if d.Priority == 0 {
		d.Priority = 100
	}

	id, err := qm.store.EnqueueDownload(d)
	if err != nil {
		return 0, fmt.Errorf("enqueueing download: %w", err)
	}
	d.ID = id

	qm.log.Printf("enqueued download %d: %s from %s on %s/%s",
		id, d.Filename, d.Bot, d.ServerAddress, d.Channel)

	// Emit event
	qm.emitEvent(Event{
		Type:          EventDownloadQueued,
		DownloadID:    id,
		Bot:           d.Bot,
		ServerAddress: d.ServerAddress,
		Channel:       d.Channel,
		Filename:      d.Filename,
		FileSize:      d.FileSize,
	})

	// Try to start the download immediately
	qm.tryDispatch()

	return id, nil
}

// CancelDownload cancels a download by its ID. If the download is active,
// its context is cancelled. If it's queued, it's just removed from the queue.
// The download record is updated in the store.
func (qm *QueueManager) CancelDownload(id int64, reason string) error {
	qm.mu.Lock()
	cancelFn, active := qm.activeJobs[id]
	if active {
		delete(qm.activeJobs, id)
		qm.globalCount--
	}
	qm.mu.Unlock()

	if active {
		cancelFn()
		qm.log.Printf("cancelled active download %d: %s", id, reason)
	}

	// Update store
	d, err := qm.store.GetDownload(id)
	if err != nil || d == nil {
		return err
	}

	// If it was active but not yet completed, mark it as queued for retry
	if active && d.Status == store.DownloadStatusDownloading {
		_ = qm.store.RequeueDownload(id)
	}

	return nil
}

// PauseDownload pauses a download. If it's currently downloading, the
// context is cancelled (the partial file remains for potential resume).
func (qm *QueueManager) PauseDownload(id int64) error {
	qm.mu.Lock()
	cancelFn, active := qm.activeJobs[id]
	if active {
		delete(qm.activeJobs, id)
		qm.globalCount--
	}
	qm.mu.Unlock()

	if active {
		cancelFn()
	}

	err := qm.store.MarkDownloadPaused(id)
	if err != nil {
		return err
	}

	d, _ := qm.store.GetDownload(id)

	// Release channel slot if this was active
	if active && d != nil {
		qm.releaseChannelSlot(d.Channel, id)
	}

	qm.emitEvent(Event{
		Type:       EventDownloadPaused,
		DownloadID: id,
	})

	// Try to dispatch next download
	qm.tryDispatch()

	return nil
}

// ResumeDownload resumes a paused or failed download by re-queueing it.
func (qm *QueueManager) ResumeDownload(id int64) error {
	err := qm.store.RetryDownload(id)
	if err != nil {
		return err
	}

	qm.log.Printf("resumed download %d", id)
	qm.tryDispatch()
	return nil
}

// RemoveDownload removes a download from the queue entirely.
func (qm *QueueManager) RemoveDownload(id int64) error {
	qm.mu.Lock()
	cancelFn, active := qm.activeJobs[id]
	if active {
		delete(qm.activeJobs, id)
		qm.globalCount--
	}
	qm.mu.Unlock()

	if active {
		cancelFn()
	}

	d, _ := qm.store.GetDownload(id)
	if d != nil && active {
		qm.releaseChannelSlot(d.Channel, id)
	}

	err := qm.store.DeleteDownload(id)
	if err != nil {
		return err
	}

	qm.emitEvent(Event{
		Type:       EventDownloadRemoved,
		DownloadID: id,
	})

	if active {
		qm.tryDispatch()
	}

	return nil
}

// BulkAction performs an action on multiple downloads.
// actions: "pause", "resume", "remove"
// Returns per-ID results.
func (qm *QueueManager) BulkAction(ids []int64, action string) (map[int64]string, error) {
	results := make(map[int64]string)

	for _, id := range ids {
		var err error
		switch strings.ToLower(action) {
		case "pause":
			err = qm.PauseDownload(id)
		case "resume":
			err = qm.ResumeDownload(id)
		case "remove":
			err = qm.RemoveDownload(id)
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

	qm.emitEvent(Event{
		Type: EventDownloadBulkResult,
	})

	return results, nil
}

// GetActiveCount returns the number of currently active downloads.
func (qm *QueueManager) GetActiveCount() int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return qm.globalCount
}

// GetActiveIDs returns the IDs of all currently active downloads.
func (qm *QueueManager) GetActiveIDs() []int64 {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	ids := make([]int64, 0, len(qm.activeJobs))
	for id := range qm.activeJobs {
		ids = append(ids, id)
	}
	return ids
}

// ---------------------------------------------------------------------------
// Internal dispatch logic
// ---------------------------------------------------------------------------

// tryDispatch checks the queue and starts as many downloads as possible
// up to the per-channel and global limits.
func (qm *QueueManager) tryDispatch() {
	// Check if we're shutting down
	select {
	case <-qm.ctx.Done():
		return
	default:
	}

	// Check disk space before dispatching
	qm.mu.RLock()
	diskLow := qm.diskLow
	qm.mu.RUnlock()
	if diskLow {
		return
	}

	// Check fresh disk space if monitor is active
	if qm.diskMon != nil {
		_, _, low, err := qm.diskMon.Check()
		if err == nil && low {
			qm.mu.Lock()
			qm.diskLow = true
			qm.mu.Unlock()
			return
		}
	}

	maxParallel := qm.cfg.Download.MaxParallelTotal
	if maxParallel < 1 {
		maxParallel = 5
	}

	qm.mu.RLock()
	activeCount := qm.globalCount
	qm.mu.RUnlock()

	if activeCount >= maxParallel {
		return // At global limit
	}

	// Get all queued downloads, ordered by priority then creation time
	queue, err := qm.store.GetQueue()
	if err != nil {
		qm.log.Printf("WARNING: failed to get queue: %v", err)
		return
	}

	for _, d := range queue {
		if d.Status != store.DownloadStatusQueued {
			continue
		}
		if activeCount >= maxParallel {
			break
		}

		// Normalize channel for consistent slot checking
		normalizedCh := normalizeChannel(d.Channel)
		
		qm.mu.RLock()
		_, channelBusy := qm.channelSlots[normalizedCh]
		qm.mu.RUnlock()

		if channelBusy {
			continue // Channel already has an active download
		}

		// Start this download
		qm.startDownload(d)
		activeCount++
		qm.mu.RLock()
		activeCount = qm.globalCount
		qm.mu.RUnlock()
	}
}

// startDownload begins a download in a new goroutine.
func (qm *QueueManager) startDownload(d store.DownloadRecord) {
	// Mark as downloading in store
	if err := qm.store.MarkDownloadStarted(d.ID); err != nil {
		qm.log.Printf("WARNING: marking download %d as started: %v", d.ID, err)
		return
	}

	ctx, cancel := context.WithCancel(qm.ctx)

	// Normalize channel for consistent slot tracking
	normalizedCh := normalizeChannel(d.Channel)

	qm.mu.Lock()
	qm.activeJobs[d.ID] = cancel
	qm.channelSlots[normalizedCh] = d.ID
	qm.globalCount++
	qm.mu.Unlock()

	qm.log.Printf("started download %d: %s from %s on %s/%s",
		d.ID, d.Filename, d.Bot, d.ServerAddress, d.Channel)

	qm.emitEvent(Event{
		Type:          EventDownloadStarted,
		DownloadID:    d.ID,
		Bot:           d.Bot,
		ServerAddress: d.ServerAddress,
		Channel:       d.Channel,
		Filename:      d.Filename,
		FileSize:      d.FileSize,
	})

	// Prepare worker config
	wCfg := DownloadConfig{
		TempDir:        qm.cfg.Download.TempDir,
		DestDir:        qm.cfg.Download.DestDir,
		ConflictPolicy: qm.cfg.Download.ConflictPolicy,
		MaxRateBPS:     qm.cfg.Download.MaxRateBPS,
		Nickname:       qm.cfg.IRC.Nickname,
		Logger:         xdccirc.LoggerFunc(qm.log.Printf),
	}

	// Track download goroutine for clean shutdown
	qm.downloadWg.Add(1)
	go func() {
		defer qm.downloadWg.Done()
		
		// Progress callback: update store and emit events
		progressFn := func(bytesReceived, totalBytes int64, speedBPS float64) {
			// Update store
			_ = qm.store.UpdateDownloadProgress(d.ID, bytesReceived, int64(speedBPS))

			// Emit progress event
			qm.emitEvent(Event{
				Type:          EventDownloadProgress,
				DownloadID:    d.ID,
				ProgressBytes: bytesReceived,
				FileSize:      totalBytes,
				SpeedBPS:      speedBPS,
				Filename:      d.Filename,
			})
		}

		// Completion callback
		completeFn := func(result workerResult) {
			// Release slot and remove from active jobs.
			// globalCount is decremented here regardless of who triggered
			// the completion (normal finish, pause, or cancel).
			qm.mu.Lock()
			delete(qm.activeJobs, d.ID)
			qm.globalCount--
			qm.mu.Unlock()
			qm.releaseChannelSlot(d.Channel, d.ID)

			// Check if the store status was changed externally (e.g. paused)
			// before we overwrite it. If the user explicitly paused or
			// removed the download, respect that decision.
			current, err := qm.store.GetDownload(d.ID)
			if err == nil && current != nil {
				if current.Status == store.DownloadStatusPaused {
					// User explicitly paused it — don't overwrite status
					return
				}
			} else if current == nil {
				// Record was deleted (removed from queue) — nothing to update
				return
			}

			if result.Error != nil {
				// Download failed
				errStr := result.Error.Error()
				_ = qm.store.MarkDownloadFailed(d.ID, errStr)

				qm.log.Printf("download %d failed: %s (%s)", d.ID, d.Filename, errStr)

				// Emit failure event
				qm.emitEvent(Event{
					Type:          EventDownloadFailed,
					DownloadID:    d.ID,
					Bot:           d.Bot,
					ServerAddress: d.ServerAddress,
					Channel:       d.Channel,
					Filename:      d.Filename,
					ErrorMessage:  errStr,
				})

				// Try fallback or next from queue
				qm.handleFallback(d, result)
			} else if result.Skipped {
				// File was skipped because destination already exists
				_ = qm.store.MarkDownloadSkipped(d.ID)

				qm.log.Printf("download %d skipped: %s already exists at %s", d.ID, d.Filename, result.FilePath)

				qm.emitEvent(Event{
					Type:          EventDownloadSkipped,
					DownloadID:    d.ID,
					Bot:           d.Bot,
					ServerAddress: d.ServerAddress,
					Channel:       d.Channel,
					Filename:      d.Filename,
					FileSize:      result.FileSize,
				})
			} else {
				// Download completed successfully
				_ = qm.store.MarkDownloadCompleted(d.ID)

				qm.log.Printf("download %d completed: %s -> %s", d.ID, d.Filename, result.FilePath)

				// Emit completion event
				qm.emitEvent(Event{
					Type:          EventDownloadCompleted,
					DownloadID:    d.ID,
					Bot:           d.Bot,
					ServerAddress: d.ServerAddress,
					Channel:       d.Channel,
					Filename:      d.Filename,
					FileSize:      result.FileSize,
				})
			}

			// Try to dispatch the next download for this channel
			qm.tryDispatch()
		}

		runDownload(ctx, d, wCfg, progressFn, completeFn)
	}()
}

// releaseChannelSlot removes a channel from the active slots map if the
// download ID matches.
func (qm *QueueManager) releaseChannelSlot(channel string, downloadID int64) {
	// Normalize channel for consistent slot release
	normalizedCh := normalizeChannel(channel)
	
	qm.mu.Lock()
	defer qm.mu.Unlock()
	
	if existingID, ok := qm.channelSlots[normalizedCh]; ok && existingID == downloadID {
		delete(qm.channelSlots, normalizedCh)
	}
}

// ---------------------------------------------------------------------------
// Fallback handling (Fase 4.11)
// ---------------------------------------------------------------------------

// handleFallback attempts to find and start an alternative download for a
// failed job, based on the configured fallback mode (Fase 9.7).
//
// Guardrails:
//   - Max retry attempts per download (configurable, default 3)
//   - No auto-retry if mode is "suggest_only"
//   - Clear tracking of fallback reason in log
func (qm *QueueManager) handleFallback(original store.DownloadRecord, result workerResult) {
	mode := qm.cfg.Download.FailFallback
	if mode != "auto_retry_best" {
		// In "suggest_only" mode, log a suggestion but don't auto-start
		qm.log.Printf("fallback: download %d failed, mode is %q (no auto-retry); suggestion: consider alternative pack for %q",
			original.ID, mode, original.Filename)
		return
	}

	// Check max retry attempts guardrail
	maxRetries := qm.cfg.Download.MaxRetryAttempts
	if maxRetries < 1 {
		maxRetries = 3
	}

	// Count existing failed attempts by checking if the record was already retried
	current, err := qm.store.GetDownload(original.ID)
	if err != nil || current == nil {
		return
	}

	// We track retry count via the priority field (incremented on each auto-retry)
	retryCount := 0
	if current.Priority > 100 {
		retryCount = current.Priority - 100
	}

	if retryCount >= maxRetries {
		qm.log.Printf("fallback: download %d failed permanently after %d retries (max %d)",
			original.ID, retryCount, maxRetries)
		qm.emitEvent(Event{
			Type:          EventDownloadFailed,
			DownloadID:    original.ID,
			Bot:           original.Bot,
			ServerAddress: original.ServerAddress,
			Channel:       original.Channel,
			Filename:      original.Filename,
			ErrorMessage:  fmt.Sprintf("failed after %d retries: %v", retryCount, result.Error),
		})
		return
	}

	// Re-queue with incremented retry count
	newPriority := current.Priority + 1
	_ = qm.store.SetDownloadPriority(original.ID, newPriority)

	qm.log.Printf("fallback: auto-retrying download %d (attempt %d/%d)",
		original.ID, retryCount+1, maxRetries)

	if err := qm.store.RetryDownload(original.ID); err != nil {
		qm.log.Printf("fallback: retry failed for download %d: %v", original.ID, err)
	}

	qm.emitEvent(Event{
		Type:          EventDownloadAlternative,
		DownloadID:    original.ID,
		Filename:      original.Filename,
		ErrorMessage:  fmt.Sprintf("auto-retry attempt %d/%d", retryCount+1, maxRetries),
	})
}

// ---------------------------------------------------------------------------
// Bandwidth management helpers (Fase 4.12)
// ---------------------------------------------------------------------------

// GetEffectiveMaxRate returns the effective max rate for the current time slot.
// This respects time-based bandwidth profiles (quiet hours).
// For now, it returns the configured max_rate_bps directly.
func (qm *QueueManager) GetEffectiveMaxRate() int64 {
	return qm.cfg.Download.MaxRateBPS
}
