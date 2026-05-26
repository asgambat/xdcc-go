<script>
  import { onMount } from 'svelte';
  import { presets, watchlists } from '../lib/stores.js';
  import { PresetsAPI, WatchlistsAPI } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let { openModal = () => {} } = $props();

  let loading = $state(true);
  let editingId = $state(null);
  let form = $state({ name: '', query: '', providers: [], min_size: '', max_size: '' });

  onMount(async () => { await load(); loading = false; });

  async function load() {
    try { presets.set(await PresetsAPI.list()); } catch {}
  }

  function resetForm() { form = { name: '', query: '', providers: [], min_size: '', max_size: '' }; editingId = null; }

  function startEdit(preset) {
    editingId = preset.id;
    form = {
      name: preset.name || '',
      query: preset.query || '',
      providers: preset.providers || [],
      min_size: preset.min_size || '',
      max_size: preset.max_size || '',
    };
  }

  async function save() {
    if (!form.name.trim()) return addToast('Enter a name', 'warning');
    const payload = {
      name: form.name.trim(),
      query: form.query.trim(),
      providers: form.providers,
      min_size: form.min_size,
      max_size: form.max_size,
    };
    try {
      if (editingId) {
        await PresetsAPI.update(editingId, payload);
        addToast('Preset updated', 'success');
      } else {
        await PresetsAPI.create(payload);
        addToast('Preset created', 'success');
      }
      resetForm();
      await load();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function remove(id) {
    try { await PresetsAPI.remove(id); addToast('Preset removed', 'info'); await load(); }
    catch (e) { addToast(e.message, 'error'); }
  }

  async function applyPreset(preset) {
    window.location.hash = `search?q=${encodeURIComponent(preset.query)}${preset.min_size ? `&min=${preset.min_size}` : ''}${preset.max_size ? `&max=${preset.max_size}` : ''}`;
    addToast('Preset applied to search', 'success');
  }

  async function loadWatchlists() {
    try { watchlists.set(await WatchlistsAPI.list()); } catch {}
  }

  function createWatchlistFromPreset(preset) {
    const esc = (s) => (s || '').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    const pName = esc(preset.name);
    const pQuery = esc(preset.query);
    const pMin = esc(preset.min_size || '');
    const pMax = esc(preset.max_size || '');

    const html = `
      <div class="form-group">
        <label class="form-label">Name</label>
        <input id="wl-name" class="form-input" value="${pName} Watchlist" placeholder="e.g. Ubuntu ISOs Monitor" />
      </div>
      <div class="form-group">
        <label class="form-label">Query</label>
        <input id="wl-query" class="form-input" value="${pQuery}" placeholder="e.g. Ubuntu 24.04" />
      </div>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label">Interval (minutes)</label>
          <input id="wl-interval" class="form-input" type="number" min="5" value="60" />
        </div>
        <div class="form-group">
          <label class="form-label">Min Size</label>
          <input id="wl-min-size" class="form-input" value="${pMin}" placeholder="optional" />
        </div>
        <div class="form-group">
          <label class="form-label">Max Size</label>
          <input id="wl-max-size" class="form-input" value="${pMax}" placeholder="optional" />
        </div>
      </div>
      <div class="flex gap-2 mb-1" style="align-items:center">
        <label style="display:flex;align-items:center;gap:0.4rem;cursor:pointer">
          <input id="wl-notify" type="checkbox" checked /> Notify on new results
        </label>
        <label style="display:flex;align-items:center;gap:0.4rem;cursor:pointer">
          <input id="wl-auto-enqueue" type="checkbox" /> Auto-enqueue
        </label>
        <label style="display:flex;align-items:center;gap:0.4rem;cursor:pointer">
          <input id="wl-enabled" type="checkbox" checked /> Enabled
        </label>
      </div>
      <div class="modal-actions">
        <button class="btn btn-ghost" onclick="window.__closeModal()">Cancel</button>
        <button class="btn btn-primary" onclick="window.__createWatchlist()">Create Watchlist</button>
      </div>`;

    window.__createWatchlist = async () => {
      const name = document.getElementById('wl-name')?.value.trim();
      const query = document.getElementById('wl-query')?.value.trim();
      const interval = parseInt(document.getElementById('wl-interval')?.value) || 60;
      const min_size = document.getElementById('wl-min-size')?.value.trim() || '';
      const max_size = document.getElementById('wl-max-size')?.value.trim() || '';
      const notify = document.getElementById('wl-notify')?.checked ?? true;
      const auto_enqueue = document.getElementById('wl-auto-enqueue')?.checked ?? false;
      const enabled = document.getElementById('wl-enabled')?.checked ?? true;

      if (!name || !query) {
        addToast('Name and query are required', 'warning');
        return;
      }

      try {
        await WatchlistsAPI.create({ name, query, interval_minutes: interval, min_size, max_size, notify, auto_enqueue, enabled });
        addToast(`Watchlist "${name}" created from preset`, 'success');
        window.__closeModal();
        await loadWatchlists();
      } catch (e) {
        addToast(e.message, 'error');
      }
    };

    openModal(`Create Watchlist from "${esc(preset.name)}"`, html);
  }
</script>

{#if loading}
  <div class="spinner"></div>
{:else}
  <div class="card mb-2">
    <div class="card-header">
      <span class="card-title">{editingId ? 'Edit Preset' : 'Create Preset'}</span>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label">Name</label>
        <input class="form-input" bind:value={form.name} placeholder="e.g. Ubuntu ISOs" />
      </div>
      <div class="form-group">
        <label class="form-label">Query</label>
        <input class="form-input" bind:value={form.query} placeholder="e.g. Ubuntu 24.04" />
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
    <div class="btn-group">
      <button class="btn btn-primary" onclick={save}>{editingId ? 'Update' : 'Create'}</button>
      {#if editingId}<button class="btn btn-ghost" onclick={resetForm}>Cancel</button>{/if}
    </div>
  </div>

  {#if $presets.length > 0}
    <div class="table-container">
      <table>
        <thead><tr><th>Name</th><th>Query</th><th>Filters</th><th>Actions</th></tr></thead>
        <tbody>
          {#each $presets as p}
            <tr>
              <td><strong>{p.name}</strong></td>
              <td class="text-sm"><code>{p.query}</code></td>
              <td class="text-sm text-muted">
                {#if p.min_size}≥{p.min_size}{/if}
                {#if p.max_size} ≤{p.max_size}{/if}
                {#if !p.min_size && !p.max_size}none{/if}
              </td>
              <td>
                <div class="btn-group">
                  <button class="btn btn-sm btn-primary" onclick={() => applyPreset(p)}>🔍 Search</button>
                  <button class="btn btn-sm btn-ghost" onclick={() => startEdit(p)}>✏️</button>
                  <button class="btn btn-sm btn-ghost" onclick={() => createWatchlistFromPreset(p)} title="Create watchlist from preset">👁️</button>
                  <button class="btn btn-sm btn-ghost" onclick={() => remove(p.id)}>🗑️</button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    <div class="empty-state">
      <div class="empty-state-text">No presets yet</div>
      <div class="empty-state-sub">Create search presets to quickly search for common queries</div>
    </div>
  {/if}
{/if}
