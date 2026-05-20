<script>
  import { currentView, downloadsBadge } from '../lib/stores.js';
  import { createEventDispatcher } from 'svelte';

  export let sidebarOpen;
  export let toggleSidebar;

  const dispatch = createEventDispatcher();

  const navItems = [
    { view: 'dashboard',  icon: '📊', label: 'Dashboard', section: 'Overview' },
    { view: 'servers',    icon: '🖥️', label: 'Servers',   section: 'Overview' },
    { view: 'downloads',  icon: '⬇️', label: 'Downloads', section: 'Overview', badge: true },
    { view: 'search',     icon: '🔍', label: 'Search',    section: 'Search' },
    { view: 'presets',    icon: '📋', label: 'Presets',   section: 'Search' },
    { view: 'watchlists', icon: '👁️', label: 'Watchlists',section: 'Search' },
    { view: 'providers',  icon: '🌐', label: 'Providers', section: 'System' },
    { view: 'settings',   icon: '⚙️', label: 'Settings',  section: 'System' },
  ];

  function navigate(view) {
    dispatch('navigate', view);
  }
</script>

<aside class="sidebar" class:open={sidebarOpen}>
  <div class="sidebar-header">
    <div class="sidebar-logo">⚡</div>
    <div class="sidebar-title">XDCC Manager</div>
    <button class="hamburger" onclick={toggleSidebar} aria-label="Close sidebar">✕</button>
  </div>
  <nav class="sidebar-nav">
    {#each navItems as item}
      {#if item.section !== navItems[navItems.indexOf(item) - 1]?.section}
        <div class="nav-section">{item.section}</div>
      {/if}
      <div
        class="nav-item"
        class:active={$currentView === item.view}
        onclick={() => navigate(item.view)}
        role="button"
        tabindex="0"
        onkeydown={(e) => e.key === 'Enter' && navigate(item.view)}
      >
        <span class="nav-icon">{item.icon}</span>
        {item.label}
        {#if item.badge && $downloadsBadge > 0}
          <span class="nav-badge">{$downloadsBadge}</span>
        {/if}
      </div>
    {/each}
  </nav>
</aside>

<style>
  .sidebar {
    width: var(--sidebar-width);
    background: var(--bg-secondary);
    border-right: 1px solid var(--border-color);
    display: flex;
    flex-direction: column;
    position: fixed;
    top: 0;
    left: 0;
    height: 100vh;
    z-index: 100;
    transition: transform var(--transition);
  }
  .sidebar-header {
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--border-color);
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-height: var(--header-height);
  }
  .sidebar-logo {
    width: 32px; height: 32px;
    background: linear-gradient(135deg, var(--accent), #a78bfa);
    border-radius: 8px;
    display: flex; align-items: center; justify-content: center;
    font-size: 1.1rem; flex-shrink: 0;
  }
  .sidebar-title { font-size: 1rem; font-weight: 600; white-space: nowrap; }
  .sidebar-nav { flex: 1; overflow-y: auto; padding: 0.75rem 0; }
  .nav-section {
    padding: 0.5rem 1.25rem 0.25rem;
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-muted);
    font-weight: 600;
  }
  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.6rem 1.25rem;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all var(--transition);
    border-left: 3px solid transparent;
    font-size: 0.9rem;
  }
  .nav-item:hover { background: var(--bg-hover); color: var(--text-primary); }
  .nav-item.active { background: var(--accent-glow); color: var(--accent-light); border-left-color: var(--accent); }
  .nav-icon { font-size: 1.1rem; width: 1.5rem; text-align: center; flex-shrink: 0; }
  .nav-badge {
    margin-left: auto;
    background: var(--accent);
    color: white;
    font-size: 0.7rem;
    padding: 0.1rem 0.5rem;
    border-radius: 999px;
    font-weight: 600;
  }
  .hamburger { display: none; background: none; border: none; color: var(--text-primary); font-size: 1.5rem; cursor: pointer; padding: 0.25rem; }
  @media (max-width: 768px) {
    .sidebar { transform: translateX(-100%); }
    .sidebar.open { transform: translateX(0); }
    .hamburger { display: block; }
  }
</style>
