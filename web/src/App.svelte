<script>
  import { onMount, onDestroy } from 'svelte';
  import { currentView, toasts, sseStatus, stats, status, config, downloads } from './lib/stores.js';
  import { sseClient, SystemAPI, DownloadsAPI } from './lib/api.js';
  import Sidebar from './components/Sidebar.svelte';
  import ConnectionStatus from './components/ConnectionStatus.svelte';
  import Toast from './components/Toast.svelte';
  import Modal from './components/Modal.svelte';
  import Dashboard from './components/Dashboard.svelte';
  import Servers from './components/Servers.svelte';
  import Downloads from './components/Downloads.svelte';
  import Search from './components/Search.svelte';
  import Presets from './components/Presets.svelte';
  import Watchlists from './components/Watchlists.svelte';
  import Providers from './components/Providers.svelte';
  import Settings from './components/Settings.svelte';

  let sidebarOpen = false;
  let modalContent = '';
  let modalTitle = '';
  let modalVisible = false;

  function toggleSidebar() { sidebarOpen = !sidebarOpen; }

  function navigateTo(view) {
    currentView.set(view);
    window.location.hash = view;
    sidebarOpen = false;
  }

  function openModal(title, html) {
    modalTitle = title;
    modalContent = html;
    modalVisible = true;
  }

  function closeModal() { modalVisible = false; }

  function getViewFromHash() {
    return window.location.hash.replace('#', '') || 'dashboard';
  }

  // Expose navigateTo globally for inline onclick handlers in modal HTML
  if (typeof window !== 'undefined') {
    window.__navigateTo = navigateTo;
    window.__openModal = openModal;
    window.__closeModal = closeModal;
  }

  // ---- Browser Notifications (Fase 9.5) ----
  function showNotification(title, body, icon = '⚡') {
    if (!('Notification' in window)) return;
    if (Notification.permission === 'granted') {
      new Notification(`${icon} ${title}`, { body, icon: '/favicon.ico' });
    }
  }

  // ---- SSE Event -> Notification mapping ----
  let notificationHandlers = [];

  onMount(async () => {
    // Request notification permission on first visit
    if ('Notification' in window && Notification.permission === 'default') {
      Notification.requestPermission();
    }

    // Load initial data
    try {
      const [statsData, statusData, cfg, dls] = await Promise.all([
        SystemAPI.stats().catch(() => null),
        SystemAPI.status().catch(() => null),
        SystemAPI.config().catch(() => null),
        DownloadsAPI.list().catch(() => []),
      ]);
      if (dls?.downloads || dls) downloads.set(dls?.downloads || dls);
      if (statsData) stats.set(statsData);
      if (statusData) status.set(statusData);
      if (cfg) config.set(cfg);
    } catch (e) { console.warn('Initial data load:', e); }

    // Initialize SSE
    sseClient.onStatusChange = (s) => { sseStatus.set(s); };
    sseClient.connect();

    // Handle resync
    sseClient.on('resync_required', async () => {
      try {
        const [dls, s, st] = await Promise.all([
          DownloadsAPI.list(),
          SystemAPI.stats(),
          SystemAPI.status(),
        ]);
        downloads.set(dls?.downloads || dls || []);
        stats.set(s);
        status.set(st);
      } catch {}
    });

    // ---- Register SSE notification handlers (Fase 9.5) ----
    const notifyMap = {
      'download_completed': (d) => showNotification('Download Complete', `${d.filename || 'File'} downloaded successfully`, '✅'),
      'download_failed': (d) => showNotification('Download Failed', `${d.filename || 'File'}: ${d.error_message || 'Unknown error'}`, '❌'),
      'disk_space_low': () => showNotification('Low Disk Space', 'Download queue paused — running low on disk space', '⚠️'),
      'watchlist_new_results': (d) => showNotification('Watchlist: New Results', `New packs found for "${d.watchlist_name || 'watchlist'}"`, '🔔'),
    };

    for (const [eventType, handler] of Object.entries(notifyMap)) {
      const unsub = sseClient.on(eventType, handler);
      notificationHandlers.push(unsub);
    }

    // ---- SSE event -> downloads store updates ----
    const refreshEvents = ['download_queued', 'download_started', 'download_progress', 'download_completed', 'download_skipped', 'download_failed', 'download_paused', 'download_removed', 'download_bulk_action_result', 'download_alternative_found'];
    for (const evt of refreshEvents) {
      sseClient.on(evt, async () => {
        try {
          const dls = await DownloadsAPI.list();
          downloads.set(dls?.downloads || dls || []);
        } catch {}
      });
    }

    // Setup hash routing
    const view = getViewFromHash();
    currentView.set(view);

    window.addEventListener('hashchange', () => {
      currentView.set(getViewFromHash());
    });

    // Keyboard shortcuts
    window.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') closeModal();
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        navigateTo('search');
      }
    });
  });

  onDestroy(() => {
    sseClient.disconnect();
    notificationHandlers.forEach(fn => fn());
  });
</script>

<div class="app-layout">
  <Sidebar {sidebarOpen} {toggleSidebar} on:navigate={(e) => navigateTo(e.detail)} />

  <div class="mobile-header">
    <button class="hamburger" onclick={toggleSidebar} aria-label="Open sidebar">☰</button>
    <strong>XDCC Manager</strong>
  </div>

  <main class="main-content">
    <div class="page-header">
      {#if $currentView === 'dashboard'}
        <h1 class="page-title">Dashboard</h1>
        <p class="page-subtitle">Server overview and download statistics</p>
      {:else if $currentView === 'servers'}
        <h1 class="page-title">Servers</h1>
        <p class="page-subtitle">Manage IRC server connections</p>
      {:else if $currentView === 'downloads'}
        <h1 class="page-title">Downloads</h1>
        <p class="page-subtitle">Manage download queue</p>
      {:else if $currentView === 'search'}
        <h1 class="page-title">Search</h1>
        <p class="page-subtitle">Find XDCC packs across providers</p>
      {:else if $currentView === 'presets'}
        <h1 class="page-title">Search Presets</h1>
        <p class="page-subtitle">Save and reuse search configurations</p>
      {:else if $currentView === 'watchlists'}
        <h1 class="page-title">Watchlists</h1>
        <p class="page-subtitle">Monitor searches for new results</p>
      {:else if $currentView === 'providers'}
        <h1 class="page-title">Search Providers</h1>
        <p class="page-subtitle">Monitor and manage search provider health</p>
      {:else if $currentView === 'settings'}
        <h1 class="page-title">Settings</h1>
        <p class="page-subtitle">Configure the XDCC server</p>
      {/if}
    </div>

    {#if $currentView === 'dashboard'}
      <Dashboard {openModal} />
    {:else if $currentView === 'servers'}
      <Servers on:navigate />
    {:else if $currentView === 'downloads'}
      <Downloads {openModal} />
    {:else if $currentView === 'search'}
      <Search {openModal} />
    {:else if $currentView === 'presets'}
      <Presets />
    {:else if $currentView === 'watchlists'}
      <Watchlists {openModal} />
    {:else if $currentView === 'providers'}
      <Providers />
    {:else if $currentView === 'settings'}
      <Settings {openModal} />
    {/if}
  </main>
</div>

<ConnectionStatus />
<Toast />
<Modal title={modalTitle} visible={modalVisible} on:close={closeModal}>
  {@html modalContent}
</Modal>

<style>
  .app-layout { display: flex; width: 100%; min-height: 100vh; }
  .main-content {
    margin-left: var(--sidebar-width);
    flex: 1;
    padding: 1.5rem 2rem;
    max-width: calc(100vw - var(--sidebar-width));
  }
  .page-header { margin-bottom: 1.5rem; }
  .page-title { font-size: 1.5rem; font-weight: 700; margin-bottom: 0.25rem; }
  .page-subtitle { color: var(--text-secondary); font-size: 0.9rem; }
  .mobile-header {
    display: none;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-color);
    position: sticky;
    top: 0;
    z-index: 99;
  }
  .hamburger { display: none; background: none; border: none; color: var(--text-primary); font-size: 1.5rem; cursor: pointer; padding: 0.25rem; }
  @media (max-width: 768px) {
    .main-content { margin-left: 0; padding: 1rem; max-width: 100vw; }
    .mobile-header { display: flex; }
    .hamburger { display: block; }
  }
</style>
