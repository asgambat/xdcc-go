<script>
  import { stats, status, downloads, activeDownloads } from '../lib/stores.js';
  import { formatBytes, formatSpeed, formatUptime, statusBadge } from '../lib/utils.js';

  let { openModal = () => {} } = $props();

  let s = $derived($stats || {});
  let st = $derived($status || {});
  let connectedCount = $derived(st?.servers?.filter(s => s.status === 'connected').length || 0);
  let serverTotal = $derived(st?.servers?.length || 0);
  let completedToday = $derived($downloads.filter(d =>
    d.status === 'completed' && d.completed_at &&
    new Date(d.completed_at) > new Date(Date.now() - 86400000)
  ).length);
</script>

<div class="stats-grid">
  <div class="stat-card">
    <div class="stat-label">Servers Online</div>
    <div class="stat-value" class:success={connectedCount > 0} class:warning={connectedCount === 0}>
      {connectedCount}/{serverTotal}
    </div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Active Downloads</div>
    <div class="stat-value info">{$activeDownloads.length}</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Queued</div>
    <div class="stat-value warning">{Math.max(0, $downloads.filter(d => d.status === 'queued').length)}</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Completed Today</div>
    <div class="stat-value success">{completedToday}</div>
  </div>
</div>

<div class="stats-grid">
  <div class="stat-card">
    <div class="stat-label">Total Downloaded</div>
    <div class="stat-value">{formatBytes(s.total_downloaded_bytes || 0)}</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Download Speed</div>
    <div class="stat-value">{formatSpeed(s.average_speed_bps || 0)}</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Server Uptime</div>
    <div class="stat-value">{formatUptime(s.uptime_seconds || st.uptime_seconds || 0)}</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Disk Free</div>
    <div class="stat-value">{formatBytes(s.disk_free_bytes || st.disk_free_bytes || 0)}</div>
  </div>
</div>

{#if $activeDownloads.length > 0}
  <div class="card mt-2">
    <div class="card-header">
      <span class="card-title">⬇️ Currently Downloading</span>
    </div>
    <div class="table-container">
      <table>
        <thead><tr><th>File</th><th>Bot</th><th>Progress</th><th>Speed</th><th>ETA</th></tr></thead>
        <tbody>
          {#each $activeDownloads as d}
            <tr>
              <td class="truncate" style="max-width:250px">{d.filename || 'Unknown'}</td>
              <td>{d.bot || '—'}</td>
              <td style="min-width:140px">
                <div class="text-sm" style="display:flex;justify-content:space-between">
                  <span>{formatBytes(d.progress_bytes)} / {formatBytes(d.file_size)}</span>
                  <span>{d.file_size > 0 ? Math.round((d.progress_bytes / d.file_size) * 100) : 0}%</span>
                </div>
                <div class="progress-bar">
                  <div class="progress-fill" style="width:{d.file_size > 0 ? Math.min(100, (d.progress_bytes / d.file_size) * 100) : 0}%"></div>
                </div>
              </td>
              <td class="text-sm">{formatSpeed(d.speed_bps)}</td>
              <td class="text-sm">{d.file_size ? formatBytes(d.file_size - d.progress_bytes) + ' left' : '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </div>
{/if}

<div class="card mt-2">
  <div class="card-header">
    <span class="card-title">🖥️ Servers</span>
    <button class="btn btn-sm btn-primary" onclick={() => window.__navigateTo('servers')}>Manage</button>
  </div>
  {#if st?.servers?.length}
    <div class="table-container">
      <table>
        <thead><tr><th>Server</th><th>Status</th><th>Channels</th><th>Uptime</th></tr></thead>
        <tbody>
          {#each st.servers as srv}
            <tr>
              <td>{srv.address || srv.server_address}:{srv.port || 6667}</td>
              <td><span class="badge badge-{statusBadge(srv.status).cls}"><span class="badge-dot"></span>{srv.status}</span></td>
              <td>{srv.channel_count || 0}</td>
              <td>{formatUptime(srv.uptime_seconds || 0)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    <div class="empty-state"><div class="empty-state-text">No servers configured</div></div>
  {/if}
</div>
