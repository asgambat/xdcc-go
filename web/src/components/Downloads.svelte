<script>
  import { onMount } from 'svelte';
  import { downloads, selectedDownloads } from '../lib/stores.js';
  import { DownloadsAPI } from '../lib/api.js';
  import { formatBytes, formatSpeed, formatETA, statusBadge } from '../lib/utils.js';
  import { addToast } from '../lib/stores.js';
  import DownloadTable from './DownloadTable.svelte';

  let loading = $state(true);
  let activeTab = $state('active'); // 'active' | 'history'

  let historyData = $state({ downloads: [], total: 0, page: 1, pageSize: 20 });
  let historyLoading = $state(false);

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

  async function loadHistoryPage(page) {
    historyLoading = true;
    try {
      const res = await DownloadsAPI.history(page, historyData.pageSize);
      historyData = {
        downloads: res?.downloads || [],
        total: res?.total || 0,
        page: res?.page || page,
        pageSize: historyData.pageSize
      };
    } catch (e) { addToast(e.message, 'error'); }
    historyLoading = false;
  }

  function switchTab(tab) {
    activeTab = tab;
    if (tab === 'history') {
      loadHistoryPage(1);
    }
  }

  function totalPages() {
    return Math.max(1, Math.ceil(historyData.total / historyData.pageSize));
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
      selectedDownloads.set(new Set($downloads.filter(d => !['completed', 'failed', 'skipped_existing'].includes(d.status)).map(d => d.id)));
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
  async function retryDownload(id) { try { await DownloadsAPI.retry(id); addToast('Retrying', 'info'); await refresh(); if (activeTab === 'history') await loadHistoryPage(historyData.page); } catch (e) { addToast(e.message, 'error'); } }
  async function removeDownload(id) { try { await DownloadsAPI.remove(id); addToast('Removed', 'info'); await refresh(); if (activeTab === 'history') await loadHistoryPage(historyData.page); } catch (e) { addToast(e.message, 'error'); } }

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
</script>

{#if loading}
  <div class="spinner"></div>
{:else}
  <div class="tab-bar mb-2" style="display:flex; gap:0.5rem; border-bottom:1px solid var(--border-color); padding-bottom:0.5rem;">
    <button class="btn btn-sm" class:btn-primary={activeTab === 'active'} onclick={() => switchTab('active')}>Active</button>
    <button class="btn btn-sm" class:btn-primary={activeTab === 'history'} onclick={() => switchTab('history')}>History</button>
  </div>

  {#if activeTab === 'active'}
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

    {#if $downloads.length === 0}
      <div class="empty-state">
        <div class="empty-state-icon">⬇️</div>
        <div class="empty-state-text">No downloads yet</div>
        <div class="empty-state-sub">Search for packs and start downloading</div>
        <button class="btn btn-primary mt-2" onclick={() => window.__navigateTo?.('search')}>Search</button>
      </div>
    {/if}
  {:else}
    <!-- History Tab -->
    <div class="card">
      <div class="card-header">
        <span class="card-title">📜 Download History ({historyData.total})</span>
      </div>
      {#if historyLoading}
        <div class="spinner" style="margin:1rem"></div>
      {:else if historyData.downloads.length > 0}
        <div class="table-container">
          <table>
            <thead>
              <tr>
                <th>File</th>
                <th>Bot</th>
                <th>Status</th>
                <th>Size</th>
                <th>Date</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {#each historyData.downloads as d (d.id)}
                <tr>
                  <td class="truncate" style="max-width:200px" title={d.filename}>{d.filename || 'Unknown'}</td>
                  <td>{d.bot || '—'}</td>
                  <td><span class="badge badge-{statusBadge(d.status).cls}"><span class="badge-dot"></span>{d.status}</span></td>
                  <td class="text-sm">{formatBytes(d.file_size)}</td>
                  <td class="text-sm">{new Date(d.completed_at || d.created_at).toLocaleDateString()}</td>
                  <td>
                    <div class="btn-group">
                      <button class="btn btn-sm btn-primary" onclick={() => retryDownload(d.id)} title="Retry">🔄</button>
                      <button class="btn btn-sm btn-ghost" onclick={() => removeDownload(d.id)} title="Remove">🗑️</button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        {#if totalPages() > 1}
          <div class="flex gap-1 mt-2" style="justify-content:center; align-items:center;">
            <button class="btn btn-sm btn-ghost" disabled={historyData.page <= 1} onclick={() => loadHistoryPage(historyData.page - 1)}>← Prev</button>
            <span class="text-sm">Page {historyData.page} of {totalPages()}</span>
            <button class="btn btn-sm btn-ghost" disabled={historyData.page >= totalPages()} onclick={() => loadHistoryPage(historyData.page + 1)}>Next →</button>
          </div>
        {/if}
      {:else}
        <div class="empty-state">
          <div class="empty-state-text">No history yet</div>
        </div>
      {/if}
    </div>
  {/if}
{/if}
