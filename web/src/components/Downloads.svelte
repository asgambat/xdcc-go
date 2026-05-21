<script>
  import { onMount } from 'svelte';
  import { downloads, selectedDownloads } from '../lib/stores.js';
  import { DownloadsAPI } from '../lib/api.js';
  import { formatBytes, formatSpeed, formatETA, statusBadge } from '../lib/utils.js';
  import { addToast } from '../lib/stores.js';
  import DownloadTable from './DownloadTable.svelte';

  let { openModal = () => {} } = $props();
  let loading = $state(true);

  let active = $derived($downloads.filter(d => d.status === 'downloading'));
  let queued = $derived($downloads.filter(d => d.status === 'queued'));
  let paused = $derived($downloads.filter(d => d.status === 'paused'));
  let completed = $derived($downloads.filter(d => ['completed', 'failed', 'skipped_existing'].includes(d.status)));

  onMount(async () => { await refresh(); loading = false; });

  async function refresh() {
    try {
      const dls = await DownloadsAPI.list();
      downloads.set(dls?.downloads || dls || []);
    } catch (e) { console.warn(e); }
  }

  function toggleDownload(id) {
    selectedDownloads.update(s => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }

  function toggleSelectAll(e) {
    if (e.target.checked) {
      selectedDownloads.set(new Set($downloads.filter(d => !['completed', 'failed'].includes(d.status)).map(d => d.id)));
    } else {
      selectedDownloads.set(new Set());
    }
  }

  async function bulkAction(action) {
    const ids = Array.from($selectedDownloads);
    if (!ids.length) return addToast('No downloads selected', 'warning');
    try {
      const result = await DownloadsAPI.bulk(ids, action);
      const success = Object.values(result || {}).filter(v => v === 'success').length;
      addToast(`${action}: ${success}/${ids.length} succeeded`, 'success');
      selectedDownloads.set(new Set());
      await refresh();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function pauseDownload(id) { try { await DownloadsAPI.pause(id); addToast('Paused', 'info'); await refresh(); } catch (e) { addToast(e.message, 'error'); } }
  async function resumeDownload(id) { try { await DownloadsAPI.resume(id); addToast('Resumed', 'success'); await refresh(); } catch (e) { addToast(e.message, 'error'); } }
  async function retryDownload(id) { try { await DownloadsAPI.retry(id); addToast('Retrying', 'info'); await refresh(); } catch (e) { addToast(e.message, 'error'); } }
  async function removeDownload(id) { try { await DownloadsAPI.remove(id); addToast('Removed', 'info'); await refresh(); } catch (e) { addToast(e.message, 'error'); } }

  async function moveUp(id) {
    const idx = $downloads.findIndex(d => d.id === id);
    if (idx <= 0) return;
    const prev = $downloads[idx - 1];
    try { await DownloadsAPI.position(id, prev.priority + 1); addToast('Moved up', 'success'); await refresh(); }
    catch (e) { addToast(e.message, 'error'); }
  }
  async function moveDown(id) {
    const idx = $downloads.findIndex(d => d.id === id);
    if (idx < 0 || idx >= $downloads.length - 1) return;
    const next = $downloads[idx + 1];
    try { await DownloadsAPI.position(id, Math.max(1, next.priority - 1)); addToast('Moved down', 'success'); await refresh(); }
    catch (e) { addToast(e.message, 'error'); }
  }

  async function loadHistory() {
    try {
      const hist = await DownloadsAPI.history(1, 100);
      const rows = (hist?.downloads || hist || []).map(d =>
        `<tr><td class="truncate" style="max-width:200px">${d.filename || 'Unknown'}</td>
        <td><span class="badge"><span class="badge-dot"></span>${d.status}</span></td>
        <td class="text-sm">${formatBytes(d.file_size)}</td>
        <td class="text-sm">${new Date(d.completed_at || d.created_at).toLocaleDateString()}</td></tr>`
      ).join('');
      openModal('Download History', `<div class="table-container" style="max-height:400px;overflow-y:auto"><table><thead><tr><th>File</th><th>Status</th><th>Size</th><th>Date</th></tr></thead><tbody>${rows}</tbody></table></div>`);
    } catch (e) { addToast(e.message, 'error'); }
  }
</script>

{#if loading}
  <div class="spinner"></div>
{:else}
  {#if $selectedDownloads.size > 0}
    <div class="flex gap-1 mb-2" style="align-items:center">
      <span class="text-sm">{$selectedDownloads.size} selected</span>
      <button class="btn btn-sm btn-warning" onclick={() => bulkAction('pause')}>Pause</button>
      <button class="btn btn-sm btn-success" onclick={() => bulkAction('resume')}>Resume</button>
      <button class="btn btn-sm btn-danger" onclick={() => bulkAction('remove')}>Remove</button>
      <button class="btn btn-sm btn-ghost" onclick={() => selectedDownloads.set(new Set())}>Clear</button>
    </div>
  {/if}

  {#if active.length > 0}
    <div class="card mb-2">
      <div class="card-header"><span class="card-title">⬇️ Downloading ({active.length})</span></div>
      <DownloadTable items={active} selectedDownloads={$selectedDownloads} {toggleDownload} {toggleSelectAll}
        {formatBytes} {formatSpeed} {formatETA} {statusBadge}
        onPause={pauseDownload} onRemove={removeDownload} />
    </div>
  {/if}

  {#if paused.length > 0}
    <div class="card mb-2">
      <div class="card-header"><span class="card-title">⏸️ Paused ({paused.length})</span></div>
      <DownloadTable items={paused} selectedDownloads={$selectedDownloads} {toggleDownload} {toggleSelectAll}
        {formatBytes} {formatSpeed} {formatETA} {statusBadge}
        onResume={resumeDownload} onRemove={removeDownload} onMoveUp={moveUp} onMoveDown={moveDown} />
    </div>
  {/if}

  {#if queued.length > 0}
    <div class="card mb-2">
      <div class="card-header"><span class="card-title">📋 Queued ({queued.length})</span></div>
      <DownloadTable items={queued} selectedDownloads={$selectedDownloads} {toggleDownload} {toggleSelectAll}
        {formatBytes} {formatSpeed} {formatETA} {statusBadge}
        onRemove={removeDownload} onMoveUp={moveUp} onMoveDown={moveDown} />
    </div>
  {/if}

  {#if completed.length > 0}
    <div class="card mb-2">
      <div class="card-header">
        <span class="card-title">✅ Completed / Failed ({completed.length})</span>
        <button class="btn btn-sm btn-ghost" onclick={loadHistory}>View History</button>
      </div>
      <DownloadTable items={completed.slice(0, 10)} showSelect={false}
        {formatBytes} {formatSpeed} {formatETA} {statusBadge}
        onRetry={retryDownload} onRemove={removeDownload} />
    </div>
  {/if}

  {#if $downloads.length === 0}
    <div class="empty-state">
      <div class="empty-state-icon">⬇️</div>
      <div class="empty-state-text">No downloads yet</div>
      <div class="empty-state-sub">Search for packs and start downloading</div>
      <button class="btn btn-primary mt-2" onclick={() => window.__navigateTo?.('search')}>Search</button>
    </div>
  {/if}
{/if}
