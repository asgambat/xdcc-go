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
	store          store.Store
	cfg            *config.SearchConfig
	log            *log.Logger
	cache          *searchCache
	disabled       map[string]bool // runtime-disabled providers
	runtimeEnabled map[string]bool // runtime-enabled providers (overrides config allowlist)
	mu             sync.RWMutex

	// Cleanup goroutine lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// New creates a new search Aggregator.
func New(st store.Store, cfg *config.SearchConfig, logger *log.Logger) *Aggregator {
	return &Aggregator{
		store:          st,
		cfg:            cfg,
		log:            logger,
		cache:          newSearchCache(st, cfg.Cache.Enabled, cfg.Cache.FreshTTL, cfg.Cache.StaleTTL),
		disabled:       make(map[string]bool),
		runtimeEnabled: make(map[string]bool),
		done:           make(chan struct{}),
	}
}

// Start begins the cache cleanup goroutine.
func (a *Aggregator) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	go a.cleanupLoop()
	return nil
}

// Stop stops the cache cleanup goroutine.
func (a *Aggregator) Stop() {
	if a.cancel != nil {
		a.cancel()
		<-a.done
	}
}

// cleanupLoop periodically removes stale cache entries.
func (a *Aggregator) cleanupLoop() {
	defer close(a.done)

	// Run cleanup every 6 hours
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.cleanupStaleEntries()
		}
	}
}

// cleanupStaleEntries removes cache entries beyond stale TTL.
func (a *Aggregator) cleanupStaleEntries() {
	now := time.Now()

	// Cleanup in-memory cache
	a.cache.mu.Lock()
	for queryKey, providers := range a.cache.entries {
		for provider, entry := range providers {
			if now.After(entry.StaleAt) {
				delete(providers, provider)
			}
		}
		if len(providers) == 0 {
			delete(a.cache.entries, queryKey)
		}
	}
	a.cache.mu.Unlock()

	// Cleanup SQLite cache if enabled
	if a.cache.enabled && a.cache.st != nil {
		// Type-assert to SQLiteStore to access CleanupSearchCache method
		if sqlStore, ok := a.store.(*store.SQLiteStore); ok {
			deleted, err := sqlStore.CleanupSearchCache()
			if err != nil {
				a.log.Printf("WARNING: cache cleanup failed: %v", err)
			} else if deleted > 0 {
				a.log.Printf("cache cleanup: removed %d stale entries", deleted)
			}
		}
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
	runtimeEnabled := a.runtimeEnabled[name]
	a.mu.RUnlock()
	if disabled {
		return false
	}
	// Runtime enable overrides the config allowlist
	if runtimeEnabled {
		return true
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
	a.runtimeEnabled[name] = true
	a.mu.Unlock()
	a.log.Printf("search provider %q enabled", name)
}

// DisableProvider disables a provider at runtime.
func (a *Aggregator) DisableProvider(name string) {
	name = strings.ToLower(name)
	a.mu.Lock()
	delete(a.runtimeEnabled, name)
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
			ps.Status = ProviderStatusDisabled
			if a.disabled[name] {
				ps.Error = "disabled at runtime"
			} else {
				ps.Error = "disabled in config"
			}
			a.mu.RUnlock()
			result = append(result, ps)
			continue
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
	allPacks, providerStatuses, hadSuccess := a.searchLive(ctx, query, opts.Providers)
	if !hadSuccess {
		// All providers failed — try stale cache (all providers)
		key := cacheKey(query)
		staleEntries := a.cache.getStale(key)
		if staleEntries != nil {
			return a.buildResultFromCache(staleEntries, opts, ProvenanceCacheStale, key), nil
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

// searchLive runs parallel searches across all enabled providers,
// restricted to providers if non-empty.
// Returns the raw packs, per-provider statuses, and whether any succeeded.
func (a *Aggregator) searchLive(ctx context.Context, query string, providers []string) ([]*entities.XDCCPack, []ProviderStatus, bool) {
	engines := srch.AvailableEngines()
	timeout := time.Duration(a.cfg.ProviderTimeout) * time.Second
	if timeout < 1*time.Second {
		timeout = 5 * time.Second
	}

	// Build a set of requested providers (empty = all enabled)
	providerSet := make(map[string]bool, len(providers))
	hasProviderFilter := len(providers) > 0
	for _, p := range providers {
		providerSet[strings.ToLower(p)] = true
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
		// If the user selected specific providers, only search those
		if hasProviderFilter && !providerSet[strings.ToLower(engName)] {
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

			// Run with timeout using context for proper cancellation
			searchCtx, searchCancel := context.WithTimeout(ctx, timeout)
			defer searchCancel()

			start := time.Now()
			done := make(chan struct{})

			var packs []*entities.XDCCPack
			var err error

			go func() {
				packs, err = engine.Search(searchCtx, query)
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

			case <-searchCtx.Done():
				results <- engineResult{
					name:    name,
					err:     fmt.Errorf("timeout after %v", timeout),
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
	// Build provider filter set (same as searchLive)
	providerSet := make(map[string]bool, len(opts.Providers))
	hasProviderFilter := len(opts.Providers) > 0
	for _, p := range opts.Providers {
		providerSet[strings.ToLower(p)] = true
	}

	var allPacks []*entities.XDCCPack
	var providerStatuses []ProviderStatus
	var cacheAge time.Duration

	for provider, entry := range entries {
		// If user selected specific providers, skip non-selected ones
		if hasProviderFilter && !providerSet[strings.ToLower(provider)] {
			continue
		}
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
	sortPacks(filtered, opts.Query)
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
		Provider:     name,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Requests:     1,
		AvgLatencyMs: float64(latency.Milliseconds()),
		UpdatedAt:    now,
	}
	if success {
		stats.Successes = 1
	} else {
		stats.Failures = 1
	}
	_ = a.store.RecordProviderStats(stats)
}
