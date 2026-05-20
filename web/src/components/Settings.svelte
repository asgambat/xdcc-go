<script>
  import { onMount } from 'svelte';
  import { config, theme, toasts } from '../lib/stores.js';
  import { SystemAPI } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let { openModal = () => {} } = $props();

  let loading = $state(true);
  let editing = $state(false);
  let configJson = $state('');
  let exportData = $state('');

  onMount(async () => { await loadConfig(); loading = false; });

  async function loadConfig() {
    try {
      const cfg = await SystemAPI.config();
      config.set(cfg);
      configJson = JSON.stringify(cfg, null, 2);
    } catch {}
  }

  function startEdit() {
    configJson = JSON.stringify($config, null, 2);
    editing = true;
  }

  async function saveConfig() {
    try {
      const parsed = JSON.parse(configJson);
      await SystemAPI.updateConfig(parsed);
      config.set(parsed);
      editing = false;
      addToast('Config saved', 'success');
    } catch (e) { addToast(`Invalid JSON: ${e.message}`, 'error'); }
  }

  function toggleTheme() {
    $theme = $theme === 'dark' ? 'light' : 'dark';
    localStorage.setItem('xdcc-theme', $theme);
    document.documentElement.setAttribute('data-theme', $theme);
  }

  async function doExport() {
    try {
      const data = await SystemAPI.exportData();
      exportData = JSON.stringify(data, null, 2);
      openModal('Export Data', `<pre style="max-height:300px;overflow:auto;background:var(--bg-input);padding:0.75rem;border-radius:var(--radius);font-size:0.8rem">${exportData}</pre><div class="modal-actions"><button class="btn btn-sm btn-primary" onclick="navigator.clipboard.writeText(${JSON.stringify(exportData)})">Copy to Clipboard</button></div>`);
      addToast('Export ready', 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doImport() {
    const text = prompt('Paste JSON export data:');
    if (!text) return;
    try {
      const parsed = JSON.parse(text);
      await SystemAPI.importData(parsed);
      addToast('Import successful', 'success');
    } catch (e) { addToast(`Import failed: ${e.message}`, 'error'); }
  }

  async function resetSetup() {
    if (!confirm('This will reset the application setup. Continue?')) return;
    try {
      await SystemAPI.bootstrap({ address: 'irc.rizon.net', port: 6667, channels: ['#linux'] });
      addToast('Setup reset complete', 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }
</script>

{#if loading}
  <div class="spinner"></div>
{:else}
  <div class="card mb-2">
    <div class="card-header">
      <span class="card-title">⚙️ Configuration</span>
      <div class="btn-group">
        {#if editing}
          <button class="btn btn-sm btn-primary" onclick={saveConfig}>Save</button>
          <button class="btn btn-sm btn-ghost" onclick={() => { editing = false; loadConfig(); }}>Cancel</button>
        {:else}
          <button class="btn btn-sm btn-primary" onclick={startEdit}>Edit</button>
          <button class="btn btn-sm btn-ghost" onclick={loadConfig}>Refresh</button>
        {/if}
      </div>
    </div>
    {#if editing}
      <textarea class="form-input" bind:value={configJson} style="min-height:300px;font-family:monospace;font-size:0.8rem" spellcheck="false"></textarea>
    {:else}
      <pre style="max-height:400px;overflow:auto;background:var(--bg-input);padding:0.75rem;border-radius:var(--radius);font-size:0.8rem;line-height:1.4">{$config ? JSON.stringify($config, null, 2) : 'No config loaded'}</pre>
    {/if}
  </div>

  <div class="card mb-2">
    <div class="card-header"><span class="card-title">🎨 Appearance</span></div>
    <div class="flex gap-1" style="align-items:center">
      <span class="text-sm">Theme:</span>
      <button class="btn" onclick={toggleTheme}>
        {$theme === 'dark' ? '☀️ Light Mode' : '🌙 Dark Mode'}
      </button>
    </div>
  </div>

  <div class="card mb-2">
    <div class="card-header"><span class="card-title">📋 Data Management</span></div>
    <div class="btn-group">
      <button class="btn btn-sm btn-primary" onclick={doExport}>📤 Export</button>
      <button class="btn btn-sm btn-warning" onclick={doImport}>📥 Import</button>
    </div>
  </div>

  <div class="card">
    <div class="card-header"><span class="card-title">⚠️ Danger Zone</span></div>
    <p class="text-sm text-muted mb-1">Reset the application to initial setup state. This will clear servers and configurations.</p>
    <button class="btn btn-sm btn-danger" onclick={resetSetup}>🔄 Reset Setup</button>
  </div>
{/if}
