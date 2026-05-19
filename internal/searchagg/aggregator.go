package searchagg

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"xdcc-go/internal/config"
	"xdcc-go/internal/entities"
	srch "xdcc-go/internal/search"
	"xdcc-go/internal/store"
)

// ---------------------------------------------------------------------------
// Aggregator
// ---------------------------------------------------------------------------

// Aggregator runs parallel searches across multiple XDCC providers, caches
// results, applies filters, and returns paginated, deduplicated results.
type Aggregator struct {
	store    store.Store
	cfg      *config.SearchConfig
	log      *log.Logger
	cache    *searchCache
	disabled map[string]bool // runtime-disabled providers
	mu       sync.RWMutex
}

// New creates a new search Aggregator.
func New(st store.Store, cfg *config.SearchConfig, logger *log.Logger) *Aggregator {
	return &Aggregator{
		store:    st,
		cfg:      cfg,
		log:      logger,
		cache:    newSearchCache(st, cfg.Cache.Enabled, cfg.Cache.FreshTTL, cfg.Cache.StaleTTL),
		disabled: make(map[string]bool),
	}
}

// ---------------------------------------------------------------------------
// Provider enable/disable at runtime
// ---------------------------------------------------------------------------

// IsProviderEnabled returns whether a provider is enabled.
func (a *Aggregator) IsProviderEnabled(name string) bool {
	name = strings.ToLower(name)
	a.mu.RLock()
	disabled := a.disabled[name]
	a.mu.RUnlock()
	if disabled {
		return false
	}
	// If configured with an allow-list, check it
	if len(a.cfg.EnabledProviders) > 0 {
		for _, ep := range a.cfg.EnabledProviders {
			if strings.EqualFold(ep, name) {
				return true
			}
		}
		return false
	}
	// Also check it's a known provider
	return srch.EngineByName(name, false) != nil
}

// EnableProvider enables a provider at runtime.
func (a *Aggregator) EnableProvider(name string) {
	name = strings.ToLower(name)
	a.mu.Lock()
	delete(a.disabled, name)
	a.mu.Unlock()
	a.log.Printf("search provider %q enabled", name)
}

// DisableProvider disables a provider at runtime.
func (a *Aggregator) DisableProvider(name string) {
	name = strings.ToLower(name)
	a.mu.Lock()
	a.disabled[name] = true
	a.mu.Unlock()
	a.log.Printf("search provider %q disabled", name)
}

// GetProviderStates returns the current state of all known providers.
func (a *Aggregator) GetProviderStates() []ProviderStatus {
	engines := srch.AvailableEngines()
	result := make([]ProviderStatus, 0, len(engines))
	for _, name := range engines {
		ps := ProviderStatus{
			Name:   name,
			Status: ProviderStatusOK,
		}
		if !a.IsProviderEnabled(name) {
			a.mu.RLock()
			if a.disabled[name] {
				ps.Status = ProviderStatusFailed
				ps.Error = "disabled at runtime"
			} else {
				ps.Status = ProviderStatusFailed
				ps.Error = "disabled in config"
			}
			a.mu.RUnlock()
		}
		// Get stats from store
		if a.store != nil {
			stats, err := a.store.GetProviderStats(name, time.Now().Add(-24*time.Hour))
			if err == nil && len(stats) > 0 {
				latest := stats[0]
				ps.LatencyMs = int64(latest.AvgLatencyMs)
				if latest.Failures > latest.Successes {
					ps.Status = ProviderStatusFailed
					ps.Error = fmt.Sprintf("%d failures in last 24h", latest.Failures)
				}
			}
		}
		result = append(result, ps)
	}
	return result
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// Search performs an aggregated search across all enabled providers.
func (a *Aggregator) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	// Normalise query
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return &SearchResult{
			Packs:      []*entities.XDCCPack{},
			Providers:  a.GetProviderStates(),
			Provenance: ProvenanceLive,
		}, nil
	}

	// Check cache first
	if a.cfg.Cache.Enabled {
		key := cacheKey(query)
		freshEntries := a.cache.getFresh(key)
		if freshEntries != nil {
			// All providers have fresh cache — return combined results
			return a.buildResultFromCache(freshEntries, opts, ProvenanceCacheFresh, key), nil
		}
	}

	// Run parallel searches
	allPacks, providerStatuses, hadSuccess := a.searchLive(ctx, query)
	if !hadSuccess {
		// All providers failed — try stale cache
		key := cacheKey(query)
		staleEntry := a.cache.get(key, "")
		if staleEntry != nil {
			filtered := filterPacks(staleEntry.Packs, opts)
			sortPacks(filtered, query)
			paged, total := paginatePacks(filtered, opts.Page, opts.PageSize)
			totalPages := (total + opts.PageSize - 1) / opts.PageSize
			if opts.PageSize < 1 {
				opts.PageSize = 50
			}
			cacheAge := time.Since(staleEntry.FetchedAt)

			return &SearchResult{
				Packs:      paged,
				Total:      total,
				Page:       opts.Page,
				PageSize:   opts.PageSize,
				TotalPages: totalPages,
				Provenance: ProvenanceCacheStale,
				Providers:  a.GetProviderStates(),
				CacheAge:   &cacheAge,
				Warnings:   []string{"All providers unreachable — serving stale cached results"},
			}, nil
		}

		// No stale cache either
		return &SearchResult{
			Packs:      []*entities.XDCCPack{},
			Providers:  a.GetProviderStates(),
			Provenance: ProvenanceLive,
			Warnings:   []string{"All providers failed and no cached data available"},
		}, nil
	}

	return a.buildResultFromLive(allPacks, opts, providerStatuses, query), nil
}

// searchLive runs parallel searches across all enabled providers.
// Returns the raw packs, per-provider statuses, and whether any succeeded.
func (a *Aggregator) searchLive(ctx context.Context, query string) ([]*entities.XDCCPack, []ProviderStatus, bool) {
	engines := srch.AvailableEngines()
	timeout := time.Duration(a.cfg.ProviderTimeout) * time.Second
	if timeout < 1*time.Second {
		timeout = 5 * time.Second
	}

	type engineResult struct {
		name    string
		packs   []*entities.XDCCPack
		err     error
		latency time.Duration
	}

	results := make(chan engineResult, len(engines))
	var wg sync.WaitGroup

	for _, engName := range engines {
		if !a.IsProviderEnabled(engName) {
			continue
		}

		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			engine := srch.EngineByName(name, false)
			if engine == nil {
				results <- engineResult{name: name, err: fmt.Errorf("unknown engine %q", name)}
				return
			}

			// Check in-memory fresh cache first
			if a.cfg.Cache.Enabled {
				key := cacheKey(query)
				entry := a.cache.get(key, name)
				if entry != nil && entry.isFresh() {
					results <- engineResult{
						name:    name,
						packs:   entry.Packs,
						latency: 0,
					}
					return
				}
			}

			// Run with timeout
			start := time.Now()
			done := make(chan struct{})

			var packs []*entities.XDCCPack
			var err error

			go func() {
				packs, err = engine.Search(query)
				close(done)
			}()

			select {
			case <-done:
				latency := time.Since(start)
				if err != nil {
					results <- engineResult{name: name, err: err, latency: latency}
				} else {
					// Cache results
					if a.cfg.Cache.Enabled {
						a.cache.set(cacheKey(query), name, packs)
					}
					results <- engineResult{name: name, packs: packs, latency: latency}
				}

			case <-time.After(timeout):
				results <- engineResult{
					name:    name,
					err:     fmt.Errorf("timeout after %v", timeout),
					latency: timeout,
				}

			case <-ctx.Done():
				results <- engineResult{
					name:    name,
					err:     ctx.Err(),
					latency: time.Since(start),
				}
			}
		}(engName)
	}

	wg.Wait()
	close(results)

	// Collect results
	var allPacks []*entities.XDCCPack
	var providerStatuses []ProviderStatus
	hadSuccess := false

	for r := range results {
		ps := ProviderStatus{
			Name:      r.name,
			LatencyMs: r.latency.Milliseconds(),
		}

		if r.err != nil {
			errStr := r.err.Error()
			if strings.Contains(errStr, "timeout") {
				ps.Status = ProviderStatusTimeout
			} else {
				ps.Status = ProviderStatusFailed
			}
			ps.Error = errStr

			// Record failure in store
			a.recordProviderResult(r.name, false, r.latency)
		} else {
			ps.Status = ProviderStatusOK
			ps.ResultCount = len(r.packs)
			allPacks = append(allPacks, r.packs...)
			hadSuccess = true

			// Record success in store
			a.recordProviderResult(r.name, true, r.latency)
		}

		providerStatuses = append(providerStatuses, ps)
	}

	return allPacks, providerStatuses, hadSuccess
}

// buildResultFromLive creates a SearchResult from live data with filtering.
func (a *Aggregator) buildResultFromLive(
	packs []*entities.XDCCPack,
	opts SearchOptions,
	providerStatuses []ProviderStatus,
	query string,
) *SearchResult {
	// Apply user filters
	filtered := filterPacks(packs, opts)
	sortPacks(filtered, query)
	paged, total := paginatePacks(filtered, opts.Page, opts.PageSize)

	totalPages := (total + opts.PageSize - 1) / opts.PageSize
	if opts.PageSize < 1 {
		opts.PageSize = 50
		opts.Page = 1
	}

	return &SearchResult{
		Packs:      paged,
		Total:      total,
		Page:       opts.Page,
		PageSize:   opts.PageSize,
		TotalPages: totalPages,
		Provenance: ProvenanceLive,
		Providers:  providerStatuses,
	}
}

// buildResultFromCache creates a SearchResult from cached data.
func (a *Aggregator) buildResultFromCache(
	entries map[string]*cacheEntry,
	opts SearchOptions,
	provenance string,
	queryKey string,
) *SearchResult {
	var allPacks []*entities.XDCCPack
	var providerStatuses []ProviderStatus
	var cacheAge time.Duration

	for provider, entry := range entries {
		allPacks = append(allPacks, entry.Packs...)
		age := time.Since(entry.FetchedAt)
		if age > cacheAge {
			cacheAge = age
		}

		ps := ProviderStatus{
			Name:        provider,
			Status:      ProviderStatusSkippedCache,
			ResultCount: len(entry.Packs),
		}
		if entry.isFresh() {
			ps.Status = ProviderStatusSkippedCache
		} else {
			ps.Status = ProviderStatusSkippedCache
		}
		providerStatuses = append(providerStatuses, ps)
	}

	// Apply filters
	filtered := filterPacks(allPacks, opts)
	sortPacks(filtered, "")
	paged, total := paginatePacks(filtered, opts.Page, opts.PageSize)

	totalPages := (total + opts.PageSize - 1) / opts.PageSize
	if opts.PageSize < 1 {
		opts.PageSize = 50
	}

	warnings := []string{}
	if provenance == ProvenanceCacheFresh {
		warnings = append(warnings, "Results from fresh cache")
	}

	return &SearchResult{
		Packs:      paged,
		Total:      total,
		Page:       opts.Page,
		PageSize:   opts.PageSize,
		TotalPages: totalPages,
		Provenance: provenance,
		Providers:  providerStatuses,
		CacheAge:   &cacheAge,
		Warnings:   warnings,
	}
}

// ---------------------------------------------------------------------------
// Provider stats recording
// ---------------------------------------------------------------------------

func (a *Aggregator) recordProviderResult(name string, success bool, latency time.Duration) {
	if a.store == nil {
		return
	}
	now := time.Now()
	windowStart := now.Truncate(1 * time.Hour)
	windowEnd := windowStart.Add(1 * time.Hour)

	stats := store.ProviderStats{
		Provider:    name,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Requests:    1,
		AvgLatencyMs: float64(latency.Milliseconds()),
		UpdatedAt:   now,
	}
	if success {
		stats.Successes = 1
	} else {
		stats.Failures = 1
	}
	_ = a.store.RecordProviderStats(stats)
}
