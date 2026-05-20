// ============================================================
// XDCC Download Manager — API Client
// ============================================================

const API_BASE = '/api';

// ---- REST Client ----
export const api = {
  async request(method, path, body = null) {
    const opts = {
      method,
      headers: { 'Content-Type': 'application/json' },
    };
    if (body !== null) {
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(`${API_BASE}${path}`, opts);
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: { message: res.statusText } }));
      throw new Error(err.error?.message || `HTTP ${res.status}`);
    }
    if (res.status === 204) return null;
    return res.json();
  },
  get(path)       { return this.request('GET', path); },
  post(path, b)   { return this.request('POST', path, b); },
  put(path, b)    { return this.request('PUT', path, b); },
  patch(path, b)  { return this.request('PATCH', path, b); },
  del(path)       { return this.request('DELETE', path); },
};

// ---- Server API ----
export const ServersAPI = {
  list()          { return api.get('/servers'); },
  connect(idOrData) {
    if (typeof idOrData === 'number' || typeof idOrData === 'string') {
      return api.post('/servers', { id: idOrData });
    }
    return api.post('/servers', idOrData);
  },
  disconnect(id)  { return api.del(`/servers/${id}`); },
  listChannels(id){ return api.get(`/servers/${id}/channels`); },
  joinChannel(sid, ch)  { return api.post(`/servers/${sid}/channels`, { name: ch }); },
  leaveChannel(sid, ch) { return api.del(`/servers/${sid}/channels/${encodeURIComponent(ch)}`); },
  topic(sid, ch)  { return api.get(`/servers/${sid}/channels/${encodeURIComponent(ch)}/topic`); },
};

// ---- Download API ----
export const DownloadsAPI = {
  list()          { return api.get('/downloads'); },
  history(page, pageSize) {
    return api.get(`/downloads/history?page=${page||1}&pageSize=${pageSize||50}`);
  },
  enqueue(d)      { return api.post('/downloads', d); },
  get(id)         { return api.get(`/downloads/${id}`); },
  remove(id)      { return api.del(`/downloads/${id}`); },
  pause(id)       { return api.post(`/downloads/${id}/pause`); },
  resume(id)      { return api.post(`/downloads/${id}/resume`); },
  retry(id)       { return api.post(`/downloads/${id}/retry`); },
  position(id, p) { return api.patch(`/downloads/${id}/position`, { priority: p }); },
  bulk(ids, action) { return api.post('/downloads/bulk', { ids, action }); },
};

// ---- Search API ----
export const SearchAPI = {
  search(params) {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== '') {
        if (Array.isArray(v)) v.forEach(x => q.append(k, x));
        else q.set(k, v);
      }
    }
    return api.get(`/search?${q.toString()}`);
  },
  parse(msg) { return api.post('/xdcc/parse', { message: msg }); },
};

// ---- Preset API ----
export const PresetsAPI = {
  list()          { return api.get('/search/presets'); },
  create(p)       { return api.post('/search/presets', p); },
  update(id, p)   { return api.put(`/search/presets/${id}`, p); },
  remove(id)      { return api.del(`/search/presets/${id}`); },
};

// ---- Watchlist API ----
export const WatchlistsAPI = {
  list()          { return api.get('/watchlists'); },
  create(w)       { return api.post('/watchlists', w); },
  update(id, w)   { return api.put(`/watchlists/${id}`, w); },
  remove(id)      { return api.del(`/watchlists/${id}`); },
  run(id)         { return api.post(`/watchlists/${id}/run`); },
};

// ---- Provider API ----
export const ProvidersAPI = {
  list()          { return api.get('/search/providers'); },
  toggle(name, enabled) { return api.patch(`/search/providers/${name}`, { enabled }); },
};

// ---- System API ----
export const SystemAPI = {
  config()        { return api.get('/config'); },
  updateConfig(c) { return api.put('/config', c); },
  stats()         { return api.get('/stats'); },
  status()        { return api.get('/status'); },
  version()       { return api.get('/version'); },
  health()        { return api.get('/healthz'); },
  ready()         { return api.get('/readyz'); },
  exportData()    { return api.post('/admin/export'); },
  importData(d)   { return api.post('/admin/import', d); },
  setupStatus()   { return api.get('/setup/status'); },
  bootstrap(c)    { return api.post('/setup/bootstrap', c); },
};

// ---- SSE Client ----
export class SSEClient {
  constructor() {
    this.eventSource = null;
    this.lastEventId = 0;
    this.listeners = {};
    this.connected = false;
    this.onStatusChange = null;
  }

  connect() {
    if (this.eventSource) this.eventSource.close();

    const url = `${API_BASE}/events`;
    this.eventSource = new EventSource(url);

    this.eventSource.addEventListener('connected', (e) => {
      try {
        const data = JSON.parse(e.data);
        this.lastEventId = data.server_id || 0;
      } catch {}
      this.connected = true;
      this._updateStatus('connected');
    });

    this.eventSource.onopen = () => {
      this.connected = true;
      this._updateStatus('connected');
    };

    this.eventSource.onerror = () => {
      this.connected = false;
      this._updateStatus('reconnecting');
    };

    const eventTypes = [
      'server_connected', 'server_disconnected', 'server_reconnecting',
      'channel_joined', 'channel_left', 'channel_topic_updated',
      'download_queued', 'download_started', 'download_progress',
      'download_completed', 'download_skipped', 'download_failed',
      'download_paused', 'download_removed', 'download_bulk_action_result',
      'download_alternative_found',
      'disk_space_low', 'disk_space_ok',
      'watchlist_new_results',
      'provider_health_changed',
      'resync_required',
    ];

    for (const type of eventTypes) {
      this.eventSource.addEventListener(type, (e) => {
        try {
          const data = JSON.parse(e.data);
          if (e.lastEventId) {
            data._eventId = parseInt(e.lastEventId);
            this.lastEventId = parseInt(e.lastEventId);
          }
          this._dispatch(type, data);
        } catch (err) {
          console.warn('SSE parse error:', err);
        }
      });
    }
  }

  disconnect() {
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
    this.connected = false;
    this._updateStatus('disconnected');
  }

  on(type, callback) {
    if (!this.listeners[type]) this.listeners[type] = [];
    this.listeners[type].push(callback);
    return () => {
      this.listeners[type] = this.listeners[type].filter(cb => cb !== callback);
    };
  }

  _dispatch(type, data) {
    const handlers = this.listeners[type] || [];
    for (const cb of handlers) {
      try { cb(data); } catch (e) { console.error('SSE handler error:', e); }
    }
    const wildcard = this.listeners['*'] || [];
    for (const cb of wildcard) {
      try { cb(type, data); } catch (e) { console.error('SSE wildcard error:', e); }
    }
  }

  _updateStatus(status) {
    if (this.onStatusChange) this.onStatusChange(status);
  }

  isConnected() { return this.connected; }
}

// Singleton SSE client
export const sseClient = new SSEClient();
