// Package searchagg aggregates XDCC pack searches across multiple providers,
// with caching, filtering, pagination, and provenance metadata.
package searchagg

import (
	"time"

	"xdcc-go/internal/entities"
)

// ---------------------------------------------------------------------------
// SearchOptions
// ---------------------------------------------------------------------------

// SearchOptions holds parameters for an aggregated search request.
type SearchOptions struct {
	Query    string   `json:"query"`
	Prefix   string   `json:"prefix,omitempty"`   // -p: filename must start with this
	Bot      string   `json:"bot,omitempty"`      // -b: bot name substring filter
	Ext      []string `json:"ext,omitempty"`      // -x: allowed extensions
	Compact  bool     `json:"compact,omitempty"`  // -c: deduplicate by bot family
	Page     int      `json:"page"`               // 1-based
	PageSize int      `json:"page_size"`          // items per page
}

// ---------------------------------------------------------------------------
// SearchResult
// ---------------------------------------------------------------------------

// SearchResult is the aggregated response from a search request.
type SearchResult struct {
	Packs      []*entities.XDCCPack `json:\"packs\"`
	Total      int                  `json:\"total\"`
	Page       int                  `json:\"page\"`
	PageSize   int                  `json:\"page_size\"`
	TotalPages int                  `json:\"total_pages\"`

	// Provenance indicates the data source: "live", "cache_fresh", "cache_stale"
	Provenance string `json:\"provenance\"`

	// Providers carries per-provider status information.
	Providers []ProviderStatus `json:\"providers\"`

	// CacheAge holds the age of the cached data when served from cache.
	CacheAge *time.Duration `json:\"cache_age,omitempty\"`

	// Warnings carries non-fatal messages (e.g. partial results).
	Warnings []string `json:\"warnings,omitempty\"`
}

// ---------------------------------------------------------------------------
// ProviderStatus
// ---------------------------------------------------------------------------

// ProviderStatus summarises the result of querying a single provider.
type ProviderStatus struct {
	Name       string `json:\"name\"`
	Status     string `json:\"status\"`     // ok | timeout | failed | skipped_cache_hit
	LatencyMs  int64  `json:\"latency_ms,omitempty"`
	ResultCount int   `json:\"result_count,omitempty"`
	Error      string `json:\"error,omitempty"`
}

// Provider status constants.
const (
	ProviderStatusOK             = "ok"
	ProviderStatusTimeout        = "timeout"
	ProviderStatusFailed         = "failed"
	ProviderStatusSkippedCache   = "skipped_cache_hit"
)

// ---------------------------------------------------------------------------
// Provenance constants
// ---------------------------------------------------------------------------

const (
	ProvenanceLive       = "live"
	ProvenanceCacheFresh = "cache_fresh"
	ProvenanceCacheStale = "cache_stale"
)
