import { writable, derived } from 'svelte/store';

export const currentView = writable('dashboard');
export const servers = writable([]);
export const channels = writable({});
export const downloads = writable([]);
export const searchResults = writable(null);
export const presets = writable([]);
export const watchlists = writable([]);
export const providers = writable([]);
export const config = writable(null);
export const stats = writable(null);
export const status = writable(null);
export const selectedDownloads = writable(new Set());
export const sseStatus = writable('disconnected');
export const theme = writable(localStorage.getItem('xdcc-theme') || 'dark');
export const toasts = writable([]);

export const activeDownloads = derived(downloads, $dls =>
  $dls.filter(d => d.status === 'downloading')
);

export const queuedDownloads = derived(downloads, $dls =>
  $dls.filter(d => d.status === 'queued')
);

export const pausedDownloads = derived(downloads, $dls =>
  $dls.filter(d => d.status === 'paused')
);

export const completedDownloads = derived(downloads, $dls =>
  $dls.filter(d => ['completed', 'failed', 'skipped_existing'].includes(d.status))
);

export const downloadsBadge = derived([activeDownloads, queuedDownloads], ([$a, $q]) =>
  $a.length + $q.length
);

export function addToast(message, type = 'info') {
  const id = Date.now() + Math.random();
  toasts.update(t => [...t, { id, message, type }]);
  setTimeout(() => {
    toasts.update(t => t.filter(x => x.id !== id));
  }, 3000);
}
