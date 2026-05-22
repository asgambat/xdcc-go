package searchagg

import (
	"path/filepath"
	"sort"
	"strings"

	"xdcc-go/internal/entities"
)

// ---------------------------------------------------------------------------
// Filter functions
// ---------------------------------------------------------------------------

// filterPacks applies the search options filters (prefix, bot, ext, compact)
// to a list of packs and returns the filtered list.
func filterPacks(packs []*entities.XDCCPack, opts SearchOptions) []*entities.XDCCPack {
	result := packs

	// Filter by query terms - all terms must be present in filename
	if opts.Query != "" {
		result = filterByQuery(result, opts.Query)
	}
	if opts.Prefix != "" {
		result = filterByPrefix(result, opts.Prefix)
	}
	if opts.Bot != "" {
		result = filterByBot(result, opts.Bot)
	}
	if len(opts.Ext) > 0 {
		result = filterByExt(result, opts.Ext)
	}
	if opts.Compact {
		result = compactPacks(result)
	}

	return result
}

// filterByQuery keeps only packs whose filename contains all query terms.
func filterByQuery(packs []*entities.XDCCPack, query string) []*entities.XDCCPack {
	// Split query into terms
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return packs
	}

	var out []*entities.XDCCPack
	for _, p := range packs {
		filenameLower := strings.ToLower(p.Filename)
		// Check if all terms are present in filename
		allFound := true
		for _, term := range terms {
			if !strings.Contains(filenameLower, term) {
				allFound = false
				break
			}
		}
		if allFound {
			out = append(out, p)
		}
	}
	return out
}

// filterByPrefix keeps only packs whose filename starts with the given prefix.
func filterByPrefix(packs []*entities.XDCCPack, prefix string) []*entities.XDCCPack {
	prefixLower := strings.ToLower(prefix)
	var out []*entities.XDCCPack
	for _, p := range packs {
		if strings.HasPrefix(strings.ToLower(p.Filename), prefixLower) {
			out = append(out, p)
		}
	}
	return out
}

// filterByBot keeps only packs whose bot name contains the substring (case-insensitive).
func filterByBot(packs []*entities.XDCCPack, bot string) []*entities.XDCCPack {
	botLower := strings.ToLower(bot)
	var out []*entities.XDCCPack
	for _, p := range packs {
		if strings.Contains(strings.ToLower(p.Bot), botLower) {
			out = append(out, p)
		}
	}
	return out
}

// filterByExt keeps only packs whose filename has one of the given extensions.
func filterByExt(packs []*entities.XDCCPack, exts []string) []*entities.XDCCPack {
	extSet := make(map[string]bool, len(exts))
	for _, e := range exts {
		e = strings.TrimSpace(e)
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		extSet[strings.ToLower(e)] = true
	}
	var out []*entities.XDCCPack
	for _, p := range packs {
		ext := strings.ToLower(filepath.Ext(p.Filename))
		if extSet[ext] {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Deduplication / compact
// ---------------------------------------------------------------------------

// compactPacks removes duplicates by (filename, size, bot family) using
// the same algorithm as the --compact flag in xdcc-browse.
// The bot family is derived via entities.BotFamily (name prefix grouping)
// so that e.g. "SubsPlease01" and "SubsPlease99" are collapsed.
func compactPacks(packs []*entities.XDCCPack) []*entities.XDCCPack {
	return entities.CompactPacks(packs)
}

// ---------------------------------------------------------------------------
// Sorting
// ---------------------------------------------------------------------------

// sortPacks sorts packs alphabetically by filename.
func sortPacks(packs []*entities.XDCCPack, query string) {
	sort.Slice(packs, func(i, j int) bool {
		a, b := packs[i], packs[j]

		// Sort alphabetically by filename
		return strings.ToLower(a.Filename) < strings.ToLower(b.Filename)
	})
}

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

// paginatePacks returns a slice of packs for the given page.
// page is 1-based. Returns the slice and total count.
func paginatePacks(packs []*entities.XDCCPack, page, pageSize int) ([]*entities.XDCCPack, int) {
	total := len(packs)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}

	start := (page - 1) * pageSize
	if start >= total {
		return []*entities.XDCCPack{}, total
	}

	end := start + pageSize
	if end > total {
		end = total
	}

	return packs[start:end], total
}
