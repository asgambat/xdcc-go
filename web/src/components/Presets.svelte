<script>
  import { onMount } from 'svelte';
  import { presets } from '../lib/stores.js';
  import { PresetsAPI } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

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
