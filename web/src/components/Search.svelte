<script>
  import { onMount } from 'svelte';
  import { searchResults } from '../lib/stores.js';
  import { SearchAPI, DownloadsAPI, ProvidersAPI } from '../lib/api.js';
  import { formatBytes, statusBadge } from '../lib/utils.js';
  import { addToast } from '../lib/stores.js';

  let { openModal = () => {} } = $props();

  let query = $state('');
  let searching = $state(false);
  let providers = $state([]);
  let selectedProviders = $state([]);
  let minSize = $state('');
  let maxSize = $state('');
  let error = $state('');
  let results = $state(null);

  onMount(async () => {
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
        return {
          name: state.name,
          enabled: insight?.enabled !== false,
        };
      }).filter(p => p.enabled);
      
      selectedProviders = providers.map(p => p.name);
    } catch (e) {
      console.error('Failed to load providers:', e);
    }
  }

  async function doSearch() {
    if (!query.trim()) return addToast('Enter a search query', 'warning');
    searching = true;
    error = '';
    try {
      const params = { query: query.trim() };
      if (selectedProviders.length > 0 && selectedProviders.length < providers.length) {
        params.providers = selectedProviders;
      }
      if (minSize) params.min_size = minSize;
      if (maxSize) params.max_size = maxSize;
      const data = await SearchAPI.search(params);
      results = data;
      searchResults.set(data);
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
      await DownloadsAPI.enqueue({
        bot: pack.bot || pack.bot_name,
        channel: pack.channel,
        pack_number: pack.pack_number || pack.number,
        filename: pack.filename,
        file_size: pack.file_size || pack.size,
        server_address: pack.server_address || pack.network,
      });
      addToast(`Download queued: ${pack.filename || pack.pack_number}`, 'success');
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
      <input class="form-input" bind:value={query} placeholder="e.g. Ubuntu 24.04" onkeydown={(e) => e.key === 'Enter' && doSearch()} />
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

  {#if providers.length > 0}
    <div class="flex gap-1" style="flex-wrap:wrap;align-items:center">
      <span class="text-sm text-muted">Providers:</span>
      {#each providers as p}
        <button class="btn btn-sm" class:btn-primary={selectedProviders.includes(p.name)} class:btn-ghost={!selectedProviders.includes(p.name)} onclick={() => toggleProvider(p.name)}>
          {p.name}
        </button>
      {/each}
    </div>
  {/if}
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
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>File</th>
              <th>Bot</th>
              <th>Channel</th>
              <th>Size</th>
              <th>Provider</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each results.packs as pack}
              <tr>
                <td class="truncate" style="max-width:250px" title={pack.filename || pack.name}>{pack.filename || pack.name || 'Unknown'}</td>
                <td>{pack.bot || pack.bot_name || '—'}</td>
                <td>{pack.channel || '—'}</td>
                <td class="text-sm">{formatBytes(pack.file_size || pack.size)}</td>
                <td><span class="badge badge-info">{pack.provider || pack.source || '?'}</span></td>
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
    {:else}
      <div class="empty-state">
        <div class="empty-state-text">No results found</div>
        <div class="empty-state-sub">Try a different search query</div>
      </div>
    {/if}
  </div>
{:else}
  <div class="empty-state">
    <div class="empty-state-icon">🔍</div>
    <div class="empty-state-text">Search for XDCC packs</div>
    <div class="empty-state-sub">Search across multiple providers (NIBL, IXIRC, SubSplease, XDCC.eu)</div>
  </div>
{/if}
