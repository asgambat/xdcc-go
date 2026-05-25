package searchagg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"xdcc-go/internal/entities"
	"xdcc-go/internal/store"
)

// ---------------------------------------------------------------------------
// WatchlistRunResult
// ---------------------------------------------------------------------------

// WatchlistRunResult holds the outcome of executing a watchlist.
type WatchlistRunResult struct {
	WatchlistID   int64  `json:"watchlist_id"`
	WatchlistName string `json:"watchlist_name"`

	// AllPacks are all packs found by the watchlist search.
	AllPacks []*entities.XDCCPack `json:"all_packs"`

	// NewPacks are packs not present in the previous run.
	NewPacks []*entities.XDCCPack `json:"new_packs"`

	// Enqueued counts how many new packs were auto-enqueued.
	Enqueued int `json:"enqueued"`

	// PreviousFingerprint is the fingerprint from the last check.
	PreviousFingerprint string `json:"previous_fingerprint"`

	// NewFingerprint is the fingerprint computed from this run.
	NewFingerprint string `json:"new_fingerprint"`

	// HasChanges is true when new packs were found.
	HasChanges bool `json:"has_changes"`
}

// ---------------------------------------------------------------------------
// RunWatchlist
// ---------------------------------------------------------------------------

// RunWatchlist executes a single watchlist: performs the search, compares
// results with the previous run's fingerprint, and optionally auto-enqueues
// new matches.
func (a *Aggregator) RunWatchlist(ctx context.Context, w store.Watchlist) (*WatchlistRunResult, error) {
	opts := SearchOptions{
		Query:    w.Query,
		PageSize: 500, // get many results for fingerprint comparison
	}

	result, err := a.Search(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("watchlist search failed: %w", err)
	}

	// Build fingerprint from all results
	fingerprint := computeFingerprint(result.Packs)
	prevFingerprint := w.LastMatchFingerprint

	wr := &WatchlistRunResult{
		WatchlistID:         w.ID,
		WatchlistName:       w.Name,
		AllPacks:            result.Packs,
		PreviousFingerprint: prevFingerprint,
		NewFingerprint:      fingerprint,
	}

	// Determine new packs
	if prevFingerprint == "" {
		// First run — no previous data to compare
		wr.HasChanges = true
		wr.NewPacks = result.Packs
	} else if fingerprint != prevFingerprint {
		wr.HasChanges = true
		// Find packs that are new (not in previous fingerprint)
		wr.NewPacks = findNewPacks(result.Packs, prevFingerprint)
	}

	// Auto-enqueue new packs
	if wr.HasChanges && w.AutoEnqueue && len(wr.NewPacks) > 0 {
		enqueued, err := a.enqueueNewPacks(ctx, wr.NewPacks)
		if err != nil {
			a.log.Warnf("watchlist %d: auto-enqueue error: %v", w.ID, err)
		}
		wr.Enqueued = enqueued
	}

	// Update the watchlist in the store
	_ = a.store.SetWatchlistChecked(ctx, w.ID, fingerprint)
	if wr.HasChanges && !w.AutoEnqueue {
		// Mark as needing notification
		_ = a.store.SetWatchlistNotified(ctx, w.ID)
	}

	return wr, nil
}

// RunAllWatchlists executes all enabled watchlists.
func (a *Aggregator) RunAllWatchlists(ctx context.Context) ([]*WatchlistRunResult, error) {
	watchlists, err := a.store.GetEnabledWatchlists(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting enabled watchlists: %w", err)
	}

	var results []*WatchlistRunResult
	for _, w := range watchlists {
		r, err := a.RunWatchlist(ctx, w)
		if err != nil {
			a.log.Warnf("watchlist %d (%s) failed: %v", w.ID, w.Name, err)
			continue
		}
		results = append(results, r)
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// Fingerprinting
// ---------------------------------------------------------------------------

// computeFingerprint creates a hash of the entire result set for change detection.
// The fingerprint uses (bot, pack_number, filename, server_address) for each pack.
func computeFingerprint(packs []*entities.XDCCPack) string {
	if len(packs) == 0 {
		return ""
	}

	// Create a stable, sorted representation
	entries := make([]string, 0, len(packs))
	for _, p := range packs {
		entries = append(entries, fmt.Sprintf("%s|%d|%s|%s",
			strings.ToLower(p.Bot),
			p.PackNumber,
			strings.ToLower(p.Filename),
			strings.ToLower(p.Server.Address),
		))
	}
	sort.Strings(entries)

	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// findNewPacks identifies packs that are new compared to the previous fingerprint.
// Since we can't reverse a hash, we compare by building a set of the previous
// entries (bot|pack_number|filename|server) — but we don't have the previous packs.
// Instead, we compute the fingerprint of the previous results and compare.
// For new pack detection, we look at packs that are different from the previous
// fingerprint by doing a coarse comparison: if the fingerprint changed, all
// packs with a different (bot, pack_number, server) combination are considered "new".
//
// A more precise approach would store the previous result set, but for now
// we flag everything as new when the fingerprint changes.
func findNewPacks(packs []*entities.XDCCPack, prevFingerprint string) []*entities.XDCCPack {
	// Compute fingerprint of current packs
	currentFp := computeFingerprint(packs)
	if currentFp == prevFingerprint {
		return nil
	}

	// Since we can't reverse the hash, return all packs when fingerprint changed.
	// The caller can decide based on WatchlistRunResult.HasChanges.
	// For a more refined implementation, we'd store the previous pack list or
	// use a bloom filter. For now, return all.
	return packs
}

// ---------------------------------------------------------------------------
// Auto-enqueue
// ---------------------------------------------------------------------------

// enqueueNewPacks automatically enqueues packs from a watchlist result.
func (a *Aggregator) enqueueNewPacks(ctx context.Context, packs []*entities.XDCCPack) (int, error) {
	enqueued := 0
	for _, p := range packs {
		// Build a pack message
		packMsg := fmt.Sprintf("xdcc send #%d", p.PackNumber)

		// Determine channel from the server (use a default for now)
		channel := "#xdcc"
		if p.Server.Address != "" {
			channel = "#xdcc" // Most XDCC channels are #xdcc
		}

		// Create download record
		d := store.DownloadRecord{
			PackMessage:   packMsg,
			Bot:           p.Bot,
			ServerAddress: p.Server.Address,
			Channel:       channel,
			Filename:      p.Filename,
			FileSize:      p.Size,
			Priority:      100,
		}

		_, err := a.store.EnqueueDownload(ctx, d)
		if err != nil {
			// Skip duplicates and other errors
			continue
		}
		enqueued++
	}
	return enqueued, nil
}
