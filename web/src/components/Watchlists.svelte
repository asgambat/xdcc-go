<script>
  import { onMount } from 'svelte';
  import { watchlists } from '../lib/stores.js';
  import { WatchlistsAPI } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let { openModal = () => {} } = $props();

  let loading = $state(true);
  let editingId = $state(null);
  let form = $state({ name: '', query: '', interval_minutes: 60, providers: [], min_size: '', max_size: '', notify: true, auto_enqueue: false, enabled: true });

  onMount(async () => { await load(); loading = false; });

  async function load() {
    try { watchlists.set(await WatchlistsAPI.list()); } catch {}
  }

  function resetForm() {
    form = { name: '', query: '', interval_minutes: 60, providers: [], min_size: '', max_size: '', notify: true, auto_enqueue: false, enabled: true };
    editingId = null;
  }

  function startEdit(w) {
    editingId = w.id;
    form = {
      name: w.name || '',
      query: w.query || '',
      interval_minutes: w.interval_minutes || 60,
      providers: w.providers || [],
      min_size: w.min_size || '',
      max_size: w.max_size || '',
      notify: w.notify !== false,
      auto_enqueue: w.auto_enqueue || false,
      enabled: w.enabled !== false,
    };
  }

  async function save() {
    if (!form.name.trim() || !form.query.trim()) return addToast('Name and query required', 'warning');
    const payload = { ...form, name: form.name.trim(), query: form.query.trim(), interval_minutes: parseInt(form.interval_minutes) || 60 };
    try {
      if (editingId) {
        await WatchlistsAPI.update(editingId, payload);
        addToast('Watchlist updated', 'success');
      } else {
        await WatchlistsAPI.create(payload);
        addToast('Watchlist created', 'success');
      }
      resetForm();
      await load();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function remove(id) {
    try { await WatchlistsAPI.remove(id); addToast('Watchlist removed', 'info'); await load(); }
    catch (e) { addToast(e.message, 'error'); }
  }

  async function runWatchlist(id) {
    try {
      const result = await WatchlistsAPI.run(id);
      addToast(`Watchlist run complete: found ${result?.new_packs?.length || result?.new_results || 0} new packs`, 'success');
      await load(); // reload to get updated results
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function downloadResult(item) {
    try {
      const { DownloadsAPI } = await import('../lib/api.js');
      await DownloadsAPI.enqueue({
        pack_message: item.pack_message,
        bot: item.bot,
        server_address: item.server_address,
        channel: item.channel || '',
        filename: item.filename,
        file_size: item.size,
      });
      addToast(`Enqueued: ${item.filename}`, 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function showResults(w) {
    const items = w.last_results || [];
    if (!items.length) return addToast('No results yet', 'info');
    // Sort: filename (asc, case-insensitive), size (asc), bot name (asc, case-insensitive)
    const sorted = [...items].sort((a, b) => {
      const fnA = (a.filename || '').toLowerCase();
      const fnB = (b.filename || '').toLowerCase();
      if (fnA < fnB) return -1;
      if (fnA > fnB) return 1;
      // Same filename — sort by size (ascending)
      const szA = a.size || 0;
      const szB = b.size || 0;
      if (szA !== szB) return szA - szB;
      // Same size — sort by bot name (ascending, case-insensitive)
      const botA = (a.bot || '').toLowerCase();
      const botB = (b.bot || '').toLowerCase();
      if (botA < botB) return -1;
      if (botA > botB) return 1;
      return 0;
    });

    // Helper: render filtered table rows
    function renderRows(filenameFilter, botFilter) {
      const ff = (filenameFilter || '').toLowerCase();
      const bf = (botFilter || '').toLowerCase();
      const filtered = ff || bf
        ? sorted.filter(item => {
            const fn = (item.filename || '').toLowerCase();
            const bn = (item.bot || '').toLowerCase();
            return fn.includes(ff) && bn.includes(bf);
          })
        : sorted;
      return {
        html: filtered.map(r =>
          `<tr>
            <td class="truncate" style="max-width:200px" title="${(r.filename || 'Unknown').replace(/"/g, '&quot;')}">${r.filename || 'Unknown'}</td>
            <td class="text-sm">${r.bot || '—'}</td>
            <td class="text-sm">${r.size ? (r.size / 1024 / 1024).toFixed(1) + ' MB' : '—'}</td>
            <td><button class="btn btn-sm btn-primary" onclick="window.__downloadResult(${JSON.stringify(r).replace(/"/g, '&quot;')})" title="Download">⬇️</button></td>
          </tr>`
        ).join(''),
        count: filtered.length
      };
    }

    // Re-render table on filter input
    window.__renderWatchlistResults = (filenameFilter, botFilter) => {
      const { html, count } = renderRows(filenameFilter, botFilter);
      const tbody = document.getElementById('wl-results-tbody');
      const countEl = document.getElementById('wl-results-count');
      if (tbody) tbody.innerHTML = html;
      if (countEl) countEl.textContent = `${count} / ${sorted.length}`;
    };

    // Expose download helper
    window.__downloadResult = (item) => downloadResult(item);

    const initial = renderRows('', '');

    const modalHtml = `
      <div class="wl-filters" style="display:flex;gap:8px;margin-bottom:8px;align-items:center">
        <input id="wl-filter-filename" placeholder="Filter by filename…"
               oninput="window.__renderWatchlistResults(this.value, document.getElementById('wl-filter-bot').value)"
               class="form-input" style="flex:1" />
        <input id="wl-filter-bot" placeholder="Filter by bot…"
               oninput="window.__renderWatchlistResults(document.getElementById('wl-filter-filename').value, this.value)"
               class="form-input" style="flex:1" />
        <span id="wl-results-count" class="text-sm text-muted" style="white-space:nowrap;display:flex;align-items:center">
          ${initial.count} / ${sorted.length}
        </span>
      </div>
      <div class="table-container" style="max-height:500px;overflow-y:auto">
        <table>
          <thead><tr><th>File</th><th>Bot</th><th>Size</th><th>Action</th></tr></thead>
          <tbody id="wl-results-tbody">${initial.html}</tbody>
        </table>
      </div>
      <div class="flex gap-1 mt-1" style="justify-content:flex-end">
        <button class="btn btn-sm btn-primary" onclick="window.__downloadAll()">⬇️ Download All (${sorted.length})</button>
      </div>`;

    openModal(`Watchlist: ${w.name} (${sorted.length} results)`, modalHtml);

    // Store items for downloadAll (re-evaluates on each click to respect active filters)
    window.__getFilteredItems = () => {
      const ff = (document.getElementById('wl-filter-filename')?.value || '').toLowerCase();
      const bf = (document.getElementById('wl-filter-bot')?.value || '').toLowerCase();
      if (!ff && !bf) return sorted;
      return sorted.filter(item => {
        const fn = (item.filename || '').toLowerCase();
        const bn = (item.bot || '').toLowerCase();
        return fn.includes(ff) && bn.includes(bf);
      });
    };
    window.__downloadAll = async () => {
      const items = window.__getFilteredItems();
      for (const item of items) {
        await downloadResult(item);
      }
      addToast(`Download all completed: ${items.length} packs enqueued`, 'success');
    };
  }
</script>

{#if loading}
  <div class="spinner"></div>
{:else}
  <div class="card mb-2">
    <div class="card-header">
      <span class="card-title">{editingId ? 'Edit Watchlist' : 'Create Watchlist'}</span>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label">Name</label>
        <input class="form-input" bind:value={form.name} placeholder="e.g. Ubuntu ISOs Monitor" />
      </div>
      <div class="form-group">
        <label class="form-label">Query</label>
        <input class="form-input" bind:value={form.query} placeholder="e.g. Ubuntu 24.04" />
      </div>
      <div class="form-group">
        <label class="form-label">Interval (minutes)</label>
        <input class="form-input" bind:value={form.interval_minutes} type="number" min="5" />
      </div>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label">Min Size</label>
        <input class="form-input" bind:value={form.min_size} placeholder="optional" />
      </div>
      <div class="form-group">
        <label class="form-label">Max Size</label>
        <input class="form-input" bind:value={form.max_size} placeholder="optional" />
      </div>
    </div>
    <div class="flex gap-2 mb-1" style="align-items:center">
      <label class="form-label" style="margin:0;display:flex;align-items:center;gap:0.4rem;cursor:pointer">
        <input type="checkbox" bind:checked={form.notify} /> Notify on new results
      </label>
      <label class="form-label" style="margin:0;display:flex;align-items:center;gap:0.4rem;cursor:pointer">
        <input type="checkbox" bind:checked={form.auto_enqueue} /> Auto-enqueue
      </label>
      <label class="form-label" style="margin:0;display:flex;align-items:center;gap:0.4rem;cursor:pointer">
        <input type="checkbox" bind:checked={form.enabled} /> Enabled
      </label>
    </div>
    <div class="btn-group">
      <button class="btn btn-primary" onclick={save}>{editingId ? 'Update' : 'Create'}</button>
      {#if editingId}<button class="btn btn-ghost" onclick={resetForm}>Cancel</button>{/if}
    </div>
  </div>

  <div class="card">
    <div class="card-header"><span class="card-title">Active Watchlists ({$watchlists.length})</span></div>
    {#if $watchlists.length > 0}
      <div class="table-container">
        <table>
          <thead><tr><th>Name</th><th>Query</th><th>Interval</th><th>Last Run</th><th>New</th><th>Actions</th></tr></thead>
          <tbody>
            {#each $watchlists as w}
              <tr>
                <td><strong>{w.name}</strong></td>
                <td><code class="text-sm">{w.query}</code></td>
                <td class="text-sm">{w.interval_minutes || 60}m</td>
                <td class="text-sm text-muted">{w.last_checked_at ? new Date(w.last_checked_at).toLocaleString() : 'never'}</td>
                <td class="text-sm" onclick={() => (w.last_results?.length || 0) > 0 && showResults(w)} style="cursor:pointer;text-decoration:underline;text-decoration-style:dotted;text-underline-offset:2px" title="Click to view results">{w.last_results?.length || 0}</td>
                <td>
                  <div class="btn-group">
                    <button class="btn btn-sm btn-primary" onclick={() => runWatchlist(w.id)} title="Run now">▶️</button>
                    {#if (w.last_results?.length || 0) > 0}
                      <button class="btn btn-sm btn-ghost" onclick={() => showResults(w)} title="Results">📋</button>
                    {/if}
                    <button class="btn btn-sm btn-ghost" onclick={() => startEdit(w)}>✏️</button>
                    <button class="btn btn-sm btn-ghost" onclick={() => remove(w.id)}>🗑️</button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <div class="empty-state">
        <div class="empty-state-text">No watchlists configured</div>
        <div class="empty-state-sub">Watchlists periodically search for new packs matching your criteria</div>
      </div>
    {/if}
  </div>
{/if}
