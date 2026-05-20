<script>
  import { onMount } from 'svelte';
  import { watchlists } from '../lib/stores.js';
  import { WatchlistsAPI } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let { openModal = () => {} } = $props();

  let loading = $state(true);
  let editingId = $state(null);
  let form = $state({ name: '', query: '', interval_minutes: 60, providers: [], min_size: '', max_size: '', notify: true, auto_download: false });

  onMount(async () => { await load(); loading = false; });

  async function load() {
    try { watchlists.set(await WatchlistsAPI.list()); } catch {}
  }

  function resetForm() {
    form = { name: '', query: '', interval_minutes: 60, providers: [], min_size: '', max_size: '', notify: true, auto_download: false };
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
      auto_download: w.auto_download || false,
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
      addToast(`Watchlist run complete: found ${result?.new_packs || result?.new_results || 0} new packs`, 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function showResults(w) {
    const items = w.last_results || w.results || [];
    if (!items.length) return addToast('No results yet', 'info');
    const rows = items.map(r =>
      `<tr><td class="truncate" style="max-width:200px">${r.filename || r.name || 'Unknown'}</td><td>${r.bot || r.bot_name || '—'}</td><td class="text-sm">${r.size ? (r.size / 1024 / 1024).toFixed(1) + ' MB' : '—'}</td></tr>`
    ).join('');
    openModal(`Watchlist: ${w.name}`, `<div class="table-container" style="max-height:400px;overflow-y:auto"><table><thead><tr><th>File</th><th>Bot</th><th>Size</th></tr></thead><tbody>${rows}</tbody></table></div>`);
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
        <input type="checkbox" bind:checked={form.auto_download} /> Auto-download
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
                <td class="text-sm">{w.interval_minutes}m</td>
                <td class="text-sm text-muted">{w.last_run_at ? new Date(w.last_run_at).toLocaleString() : 'never'}</td>
                <td class="text-sm">{(w.last_results || w.results || []).length}</td>
                <td>
                  <div class="btn-group">
                    <button class="btn btn-sm btn-primary" onclick={() => runWatchlist(w.id)} title="Run now">▶️</button>
                    {#if (w.last_results || w.results || []).length > 0}
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
