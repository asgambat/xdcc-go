<script>
  import { onMount } from 'svelte';
  import { searchResults } from '../lib/stores.js';
  import { SearchAPI, DownloadsAPI, ProvidersAPI } from '../lib/api.js';
  import { formatBytes, statusBadge } from '../lib/utils.js';
  import { addToast } from '../lib/stores.js';

  let { openModal = () => {} } = $props();

  // --- Query history ---
  const HISTORY_KEY = 'xdcc-search-history';
  const MAX_HISTORY = 10;
  let queryHistory = $state([]);

  function loadHistory() {
    try {
      queryHistory = JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]');
    } catch { queryHistory = []; }
  }

  function saveQueryToHistory(q) {
    const trimmed = q.trim();
    if (!trimmed) return;
    let hist = queryHistory.filter(h => h !== trimmed);
    hist.unshift(trimmed);
    if (hist.length > MAX_HISTORY) hist = hist.slice(0, MAX_HISTORY);
    queryHistory = hist;
    try { localStorage.setItem(HISTORY_KEY, JSON.stringify(hist)); } catch {}
  }

  let query = $state('');
  let searching = $state(false);
  let providers = $state([]);
  let selectedProviders = $state([]);
  let minSize = $state('');
  let maxSize = $state('');
  let error = $state('');
  let results = $derived($searchResults);

  // --- Client-side filters ---
  let filterName = $state('');
  let filterBot = $state('');
  let filterServer = $state('');
  let filterMinMB = $state(0);
  let filterMaxMB = $state(0);
  let compactMode = $state(false);

  // --- Slider range (min/max MB from current results) ---
  let sliderRange = $derived.by(() => {
    const packs = results?.packs;
    if (!packs?.length) return { min: 0, max: 0, hasData: false };
    let minB = Infinity, maxB = 0;
    for (const p of packs) {
      const sz = p.size ?? 0;
      if (sz > 0) {
        if (sz < minB) minB = sz;
        if (sz > maxB) maxB = sz;
      }
    }
    if (minB === Infinity) return { min: 0, max: 0, hasData: false };
    const minMB = Math.floor(minB / (1024 * 1024));
    const maxMB = Math.ceil(maxB / (1024 * 1024));
    return { min: minMB, max: maxMB, hasData: true };
  });

  // --- Sorting state ---
  let sortColumn = $state('');
  let sortDirection = $state('asc');

  // --- Active filter count for display
  let activeFilterCount = $derived.by(() => {
    let count = 0;
    if (filterName.trim()) count++;
    if (filterBot.trim()) count++;
    if (filterServer.trim()) count++;
    if (sliderRange.hasData) {
      const minActive = filterMinMB > sliderRange.min;
      const maxActive = filterMaxMB < sliderRange.max;
      if (minActive || maxActive) count++;
    }
    return count;
  });

  // --- Derived: sorted & filtered packs ---
  let sortedPacks = $derived.by(() => {
    if (!results?.packs?.length) return [];
    let packs = results.packs;

    // Apply client-side filters
    const fn = filterName.trim().toLowerCase();
    const fb = filterBot.trim().toLowerCase();
    const fs = filterServer.trim().toLowerCase();

    if (fn) packs = packs.filter(p => (p.filename || '').toLowerCase().includes(fn));
    if (fb) packs = packs.filter(p => (p.bot || '').toLowerCase().startsWith(fb));
    if (fs) packs = packs.filter(p => (p.server?.address || '').toLowerCase().includes(fs));
    if (sliderRange.hasData) {
      const minBytes = filterMinMB > sliderRange.min ? filterMinMB * 1024 * 1024 : 0;
      const maxBytes = filterMaxMB < sliderRange.max ? filterMaxMB * 1024 * 1024 : 0;
      packs = packs.filter(p => {
        const sz = p.size ?? 0;
        if (sz <= 0) return true;
        if (minBytes > 0 && sz < minBytes) return false;
        if (maxBytes > 0 && sz > maxBytes) return false;
        return true;
      });
    }

    if (!sortColumn) return packs;
    const dir = sortDirection === 'asc' ? 1 : -1;
    const sorted = [...packs].sort((a, b) => {
      let valA, valB;
      switch (sortColumn) {
        case 'filename':
          valA = (a.filename || '').toLowerCase();
          valB = (b.filename || '').toLowerCase();
          return valA < valB ? -dir : valA > valB ? dir : 0;
        case 'bot':
          valA = (a.bot || '').toLowerCase();
          valB = (b.bot || '').toLowerCase();
          return valA < valB ? -dir : valA > valB ? dir : 0;
        case 'channel':
          valA = (a.channel || '').toLowerCase();
          valB = (b.channel || '').toLowerCase();
          return valA < valB ? -dir : valA > valB ? dir : 0;
        case 'size':
          valA = a.size ?? 0;
          valB = b.size ?? 0;
          return (valA - valB) * dir;
        case 'server':
          valA = (a.server?.address || '').toLowerCase();
          valB = (b.server?.address || '').toLowerCase();
          return valA < valB ? -dir : valA > valB ? dir : 0;
        default:
          return 0;
      }
    });
    return sorted;
  });

  function toggleSort(column) {
    if (sortColumn === column) {
      // Toggle direction
      sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
    } else {
      sortColumn = column;
      sortDirection = 'asc';
    }
  }

  function sortIcon(column) {
    if (sortColumn !== column) return '↕';
    return sortDirection === 'asc' ? '▲' : '▼';
  }

  onMount(async () => {
    loadHistory();
    await loadProviders();
  });

  async function loadProviders() {
    try {
      const data = await ProvidersAPI.list();
      // Backend returns { states: [...], insights: [...] }
      const states = data?.states || [];
      const insights = data?.insights || [];
      
      // Merge states with insights
      providers = states.map(state => {
        const insight = insights.find(i => i.name === state.name);
        // If insight is available, use its enabled field.
        // If not (e.g. GetProviderInsights failed), fall back to checking
        // the state's error message for 'disabled' markers — GetProviderStates
        // already sets error="disabled in config" / "disabled at runtime"
        // for providers that are disabled in config or at runtime.
        let enabled;
        if (insight !== undefined) {
          enabled = insight.enabled;
        } else {
          enabled = !(state.error && state.error.includes('disabled'));
        }
        return { name: state.name, enabled };
      }).filter(p => p.enabled);
      
      selectedProviders = providers.map(p => p.name);
    } catch (e) {
      console.error('Failed to load providers:', e);
    }
  }

  function clearFilters() {
    filterName = '';
    filterBot = '';
    filterServer = '';
    if (sliderRange.hasData) {
      filterMinMB = sliderRange.min;
      filterMaxMB = sliderRange.max;
    } else {
      filterMinMB = 0;
      filterMaxMB = 0;
    }
  }

  async function doSearch() {
    if (!query.trim()) return addToast('Enter a search query', 'warning');
    searching = true;
    error = '';
    clearFilters();
    saveQueryToHistory(query);
    try {
      const params = { q: query.trim() };
      if (selectedProviders.length > 0 && selectedProviders.length < providers.length) {
        params.providers = selectedProviders;
      }
      if (minSize) params.min_size = minSize;
      if (maxSize) params.max_size = maxSize;
      if (compactMode) params.compact = 'true';
      const data = await SearchAPI.search(params);
      searchResults.set(data);
      // Initialize both size sliders to full range so all results are included at start
      if (data?.packs?.length) {
        let minB = Infinity, maxB = 0;
        for (const p of data.packs) {
          const sz = p.size ?? 0;
          if (sz > 0) {
            if (sz < minB) minB = sz;
            if (sz > maxB) maxB = sz;
          }
        }
        if (maxB > 0) {
          filterMinMB = Math.floor(minB / (1024 * 1024));
          filterMaxMB = Math.ceil(maxB / (1024 * 1024));
        }
      }
    } catch (e) {
      error = e.message;
      addToast(e.message, 'error');
    }
    searching = false;
  }

  async function parseAndDownload() {
    const msg = prompt('Paste an XDCC pack message to parse:');
    if (!msg) return;
    try {
      const parsed = await SearchAPI.parse(msg);
      if (!parsed?.pack) return addToast('Could not parse pack info', 'error');
      const p = parsed.pack;
      await DownloadsAPI.enqueue({
        bot: p.bot_name,
        channel: p.channel,
        pack_number: p.pack_number,
        filename: p.filename,
        file_size: p.file_size,
        server_address: p.server_address || p.network,
      });
      addToast(`Download queued: ${p.filename || p.pack_number}`, 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function downloadPack(pack) {
    try {
      // Generate pack_message in the format "xdcc send #<number>"
      const packMessage = `xdcc send #${pack.pack_number}`;
      
      await DownloadsAPI.enqueue({
        pack_message: packMessage,
        bot: pack.bot,
        channel: pack.channel || '',  // Empty channel - WHOIS will discover it
        filename: pack.filename,
        file_size: pack.size,
        server_address: pack.server?.address || 'unknown',
      });
      addToast(`Download queued: ${pack.filename}`, 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function toggleProvider(name) {
    if (selectedProviders.includes(name)) {
      selectedProviders = selectedProviders.filter(p => p !== name);
    } else {
      selectedProviders = [...selectedProviders, name];
    }
  }

  // Load providers on mount
  $effect(() => { loadProviders(); });
</script>

<div class="card mb-2">
  <div class="filters-bar">
    <div class="form-group" style="flex:1;min-width:250px">
      <label class="form-label">Search Query</label>
      <input class="form-input" bind:value={query} placeholder="e.g. Ubuntu 24.04"
        list="query-history"
        onkeydown={(e) => e.key === 'Enter' && doSearch()} />
      <datalist id="query-history">
        {#each queryHistory as h}
          <option value={h}>{h}</option>
        {/each}
      </datalist>
    </div>
    <div class="form-group" style="min-width:120px">
      <label class="form-label">Min Size</label>
      <input class="form-input" bind:value={minSize} placeholder="e.g. 100MB" />
    </div>
    <div class="form-group" style="min-width:120px">
      <label class="form-label">Max Size</label>
      <input class="form-input" bind:value={maxSize} placeholder="e.g. 4GB" />
    </div>
    <div class="form-group" style="display:flex;align-items:end">
      <button class="btn btn-primary btn-lg" onclick={doSearch} disabled={searching}>
        {searching ? '🔍 Searching...' : '🔍 Search'}
      </button>
    </div>
  </div>

  <div class="flex gap-1" style="flex-wrap:wrap;align-items:center">
    {#if providers.length > 0}
      <span class="text-sm text-muted">Providers:</span>
      {#each providers as p}
        <button class="btn btn-sm" class:btn-primary={selectedProviders.includes(p.name)} class:btn-ghost={!selectedProviders.includes(p.name)} onclick={() => toggleProvider(p.name)}>
          {p.name}
        </button>
      {/each}
      <span class="separator-dot">·</span>
    {/if}
    <label class="toggle-label" title="Collapse duplicate results sharing the same filename, size, and bot family">
      <input type="checkbox" bind:checked={compactMode} />
      <span class="toggle-text">Compact</span>
    </label>
  </div>
</div>

<div class="flex gap-1 mb-2" style="align-items:center">
  <button class="btn btn-sm btn-ghost" onclick={parseAndDownload}>📋 Parse & Download from IRC message</button>
</div>

{#if searching}
  <div class="spinner"></div>
{:else if error}
  <div class="empty-state">
    <div class="empty-state-icon">⚠️</div>
    <div class="empty-state-text">Search failed</div>
    <div class="empty-state-sub">{error}</div>
  </div>
{:else if results}
  <div class="card">
    <div class="card-header">
      <span class="card-title">Results</span>
      <span class="text-sm text-muted">{results.total_results || results.packs?.length || 0} packs found</span>
    </div>
    {#if results.packs?.length > 0}
      <!-- Client-side filter bar — always visible when results exist, even if all filtered out -->
      <div class="filter-bar">
        <div class="filter-inputs">
          <input class="form-input" bind:value={filterName} placeholder="Filter by name..." />
          <input class="form-input" bind:value={filterBot} placeholder="Filter by bot..." />
          <input class="form-input" bind:value={filterServer} placeholder="Filter by server..." />
          {#if sliderRange.hasData}
            <div class="dual-slider-group">
              <div class="dual-slider-labels">
                <span class="slider-label">Min {filterMinMB} MB</span>
                <span class="slider-label">Max {filterMaxMB} MB</span>
              </div>
              <div class="dual-slider-tracks">
                <input type="range" class="size-slider size-slider-min"
                  min={sliderRange.min} max={sliderRange.max}
                  bind:value={filterMinMB}
                  oninput={() => { if (filterMinMB > filterMaxMB) filterMinMB = filterMaxMB; }} />
                <input type="range" class="size-slider size-slider-max"
                  min={sliderRange.min} max={sliderRange.max}
                  bind:value={filterMaxMB}
                  oninput={() => { if (filterMaxMB < filterMinMB) filterMaxMB = filterMinMB; }} />
              </div>
            </div>
          {/if}
        </div>
        {#if activeFilterCount > 0}
          <button class="btn btn-sm btn-ghost" onclick={clearFilters}>
            ✕ Clear {activeFilterCount} filter{activeFilterCount !== 1 ? 's' : ''}
          </button>
        {/if}
      </div>

      {#if sortedPacks.length > 0}
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th class="sortable" onclick={() => toggleSort('filename')}>
                File <span class="sort-icon">{sortIcon('filename')}</span>
              </th>
              <th class="sortable" onclick={() => toggleSort('bot')}>
                Bot <span class="sort-icon">{sortIcon('bot')}</span>
              </th>
              <th class="sortable" onclick={() => toggleSort('channel')}>
                Channel <span class="sort-icon">{sortIcon('channel')}</span>
              </th>
              <th class="sortable" onclick={() => toggleSort('size')}>
                Size <span class="sort-icon">{sortIcon('size')}</span>
              </th>
              <th class="sortable" onclick={() => toggleSort('server')}>
                Server <span class="sort-icon">{sortIcon('server')}</span>
              </th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each sortedPacks as pack}
              <tr>
                <td class="truncate" style="max-width:250px" title={pack.filename}>{pack.filename || 'Unknown'}</td>
                <td>{pack.bot || '—'}</td>
                <td>{pack.channel || '—'}</td>
                <td class="text-sm">{formatBytes(pack.size)}</td>
                <td><span class="badge badge-info">{pack.server?.address || '?'}</span></td>
                <td>
                  <button class="btn btn-sm btn-primary" onclick={() => downloadPack(pack)}>⬇️ Download</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if results.packs.length >= 50}
        <div class="pagination">
          <span class="page-info">Showing first 50 results. Refine your search for more specific results.</span>
        </div>
      {/if}
      {:else if results.packs?.length > 0}
      <div class="empty-state">
        <div class="empty-state-text">All {results.packs.length} results filtered out</div>
        <div class="empty-state-sub">
          {#if activeFilterCount > 0}
            <button class="btn btn-sm btn-ghost" onclick={clearFilters}>✕ Clear filters to see results</button>
          {:else}
            Try adjusting the client-side filters above
          {/if}
        </div>
      </div>
    {:else}
      <div class="empty-state">
        <div class="empty-state-text">No results found</div>
        <div class="empty-state-sub">Try a different search query</div>
      </div>
    {/if}
    {/if}
  </div>
{:else}
  <div class="empty-state">
    <div class="empty-state-icon">🔍</div>
    <div class="empty-state-text">Search for XDCC packs</div>
    <div class="empty-state-sub">Search across multiple providers (NIBL, IXIRC, SubSplease, XDCC.eu)</div>
  </div>
{/if}

<style>
  th.sortable {
    cursor: pointer;
    user-select: none;
    transition: color 0.15s ease;
  }
  th.sortable:hover {
    color: var(--accent-light);
  }
  .sort-icon {
    display: inline-block;
    font-size: 0.7rem;
    margin-left: 0.25rem;
    opacity: 0.4;
    transition: opacity 0.15s ease, transform 0.15s ease;
  }
  th.sortable:hover .sort-icon {
    opacity: 0.8;
  }
  th.sortable:active .sort-icon {
    transform: scale(0.85);
  }

  .size-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 6px;
    background: var(--bg-tertiary);
    border-radius: 3px;
    outline: none;
    cursor: pointer;
  }
  .size-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--accent);
    border: 2px solid var(--accent-light);
    cursor: pointer;
    transition: transform 0.15s ease;
  }
  .size-slider::-webkit-slider-thumb:hover {
    transform: scale(1.2);
  }
  .size-slider::-moz-range-thumb {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--accent);
    border: 2px solid var(--accent-light);
    cursor: pointer;
  }

  /* --- Filter bar above results table --- */
  .filter-bar {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-lg);
    margin-bottom: 0.75rem;
    flex-wrap: wrap;
  }
  .filter-bar .filter-inputs {
    display: flex;
    gap: 0.5rem;
    flex: 1;
    flex-wrap: wrap;
    align-items: center;
  }
  .filter-bar .form-input {
    width: 180px;
    padding: 0.4rem 0.6rem;
    font-size: 0.82rem;
    height: 34px;
  }

  /* --- Dual range slider --- */
  .dual-slider-group {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 220px;
  }
  .dual-slider-labels {
    display: flex;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .slider-label {
    font-size: 0.7rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    white-space: nowrap;
  }
  .dual-slider-tracks {
    position: relative;
    height: 6px;
  }
  .dual-slider-tracks .size-slider {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 6px;
    -webkit-appearance: none;
    appearance: none;
    background: transparent;
    pointer-events: none;
    outline: none;
    margin: 0;
  }
  .dual-slider-tracks .size-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--accent);
    border: 2px solid var(--accent-light);
    cursor: pointer;
    pointer-events: auto;
    transition: transform 0.15s ease;
  }
  .dual-slider-tracks .size-slider::-webkit-slider-thumb:hover {
    transform: scale(1.2);
  }
  .dual-slider-tracks .size-slider::-moz-range-thumb {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--accent);
    border: 2px solid var(--accent-light);
    cursor: pointer;
    pointer-events: auto;
  }
  .dual-slider-tracks .size-slider::-webkit-slider-runnable-track {
    height: 6px;
    background: transparent;
  }
  .dual-slider-tracks .size-slider::-moz-range-track {
    height: 6px;
    background: transparent;
  }
  /* Visual track behind both sliders */
  .dual-slider-tracks::before {
    content: '';
    position: absolute;
    top: 50%;
    left: 0;
    right: 0;
    height: 6px;
    background: var(--bg-tertiary);
    border-radius: 3px;
    transform: translateY(-50%);
    pointer-events: none;
  }

  /* --- Compact toggle --- */
  .toggle-label {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    cursor: pointer;
    user-select: none;
    font-size: 0.82rem;
    color: var(--text-muted);
    transition: color 0.15s ease;
    white-space: nowrap;
    padding: 0.25rem 0.4rem;
    border-radius: var(--radius-sm);
    border: 1px solid transparent;
    transition: all 0.15s ease;
  }
  .toggle-label:hover {
    color: var(--text);
    border-color: var(--border-color);
  }
  .toggle-label input[type="checkbox"] {
    accent-color: var(--accent);
    width: 15px;
    height: 15px;
    cursor: pointer;
  }
  .toggle-text {
    font-weight: 500;
  }
</style>
