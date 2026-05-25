<script>
  import { onMount, onDestroy } from 'svelte';
  import { servers, channels } from '../lib/stores.js';
  import { ServersAPI, sseClient } from '../lib/api.js';
  import { statusBadge } from '../lib/utils.js';
  import { addToast } from '../lib/stores.js';

  let newAddress = $state('irc.rizon.net');
  let newPort = $state(6667);
  let loading = $state(true);

  // Track which servers are currently being connected to prevent double-clicks
  let connectingServers = $state(new Set());
  let unsubServerConnected, unsubServerDisconnected, unsubServerReconnecting;
  let unsubChannelJoined, unsubChannelLeft, unsubChannelTopicUpdated;

  // Sort servers: connected first, then disconnected, then by address.
  // Server records may use either `address` or `server_address`.
  let sorted = $derived($servers.length ? [...$servers].sort((a, b) => {
    if (a.status === 'connected' && b.status !== 'connected') return -1;
    if (a.status !== 'connected' && b.status === 'connected') return 1;
    const addrA = (a.address || a.server_address || '');
    const addrB = (b.address || b.server_address || '');
    return addrA.localeCompare(addrB);
  }) : []);

  onMount(async () => {
    await loadServers();
    loading = false;

    // When a server_connected SSE event arrives while we're waiting,
    // show the success toast and stop showing "Connecting...".
    // Also update the server status in the store for auto-refresh.
    unsubServerConnected = sseClient.on('server_connected', (data) => {
      const serverId = data.server_id;
      if (serverId) {
        // Update server status in store so the UI auto-updates
        servers.update(list => list.map(s =>
          s.id === serverId ? { ...s, status: 'connected' } : s
        ));
        // Reload channels for this server (may have changed during disconnect)
        loadChannels(serverId);
        if (connectingServers.has(serverId)) {
          connectingServers.delete(serverId);
          connectingServers = connectingServers;
          const addr = data.server_addr || '';
          addToast(addr ? `Connected to ${addr}` : 'Server connected', 'success');
        }
      }
    });

    // When server_disconnected arrives, update the server status in the store
    // so the UI auto-updates instead of waiting for a manual refresh.
    unsubServerDisconnected = sseClient.on('server_disconnected', (data) => {
      const serverId = data.server_id;
      if (serverId) {
        // Update server status in store
        servers.update(list => list.map(s =>
          s.id === serverId ? { ...s, status: 'disconnected' } : s
        ));
        if (connectingServers.has(serverId)) {
          connectingServers.delete(serverId);
          connectingServers = connectingServers;
          const addr = data.server_addr || '';
          addToast(addr ? `Connection to ${addr} failed` : 'Connection failed', 'error');
        }
      }
    });

    // When server_reconnecting arrives (e.g. connection dropped, auto-retry),
    // update the server status in the store so the UI reflects it immediately.
    unsubServerReconnecting = sseClient.on('server_reconnecting', (data) => {
      const serverId = data.server_id;
      if (serverId) {
        servers.update(list => list.map(s =>
          s.id === serverId ? { ...s, status: 'reconnecting' } : s
        ));
      }
    });

    // When a channel is joined (e.g. auto-join after connect), reload the
    // channels list so the "Joined: YES" badge and topic update in the UI.
    unsubChannelJoined = sseClient.on('channel_joined', (data) => {
      const serverId = data.server_id;
      if (serverId) {
        loadChannels(serverId);
      }
    });

    // When a channel is left, reload to update the Joined badge.
    unsubChannelLeft = sseClient.on('channel_left', (data) => {
      const serverId = data.server_id;
      if (serverId) {
        loadChannels(serverId);
      }
    });

    // When a channel topic is updated, reload the channels list so the
    // topic column reflects the new value without a full refresh.
    unsubChannelTopicUpdated = sseClient.on('channel_topic_updated', (data) => {
      const serverId = data.server_id;
      if (serverId) {
        loadChannels(serverId);
      }
    });
  });

  onDestroy(() => {
    if (unsubServerConnected) unsubServerConnected();
    if (unsubServerDisconnected) unsubServerDisconnected();
    if (unsubServerReconnecting) unsubServerReconnecting();
    if (unsubChannelJoined) unsubChannelJoined();
    if (unsubChannelLeft) unsubChannelLeft();
    if (unsubChannelTopicUpdated) unsubChannelTopicUpdated();
  });

  async function loadServers() {
    try {
      const list = await ServersAPI.list();
      servers.set(list);
      // Pre-load channels for all servers to avoid {#await} infinite loop
      await Promise.allSettled(list.map(srv => loadChannels(srv.id)));
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function connectNewServer() {
    if (!newAddress.trim()) return addToast('Enter a server address', 'warning');
    try {
      const result = await ServersAPI.connect({ address: newAddress.trim(), port: newPort });
      const serverId = result?.id;
      if (serverId) {
        connectingServers.add(serverId);
        connectingServers = connectingServers;
      }
      // Refresh the server list immediately so the new server appears,
      // and load its channels so we don't show "Loading channels..." forever.
      await loadServers();
      // Toast is shown by SSE server_connected (success) or server_disconnected (failure).
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function connectServer(id) {
    connectingServers.add(id);
    connectingServers = connectingServers; // trigger reactivity
    try {
      await ServersAPI.connect(id);
      // Wait for SSE server_connected (success) or server_disconnected (failure)
      // before showing any toast or removing from the connecting set.
    } catch (e) {
      connectingServers.delete(id);
      connectingServers = connectingServers;
      addToast(e.message, 'error');
    }
  }

  async function disconnectServer(id) {
    try {
      await ServersAPI.disconnect(id);
      addToast('Server disconnected', 'info');
      await loadServers();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function removeServer(id, address) {
    if (!window.confirm(`Remove server ${address}? This will disconnect (if connected) and delete it permanently.`)) return;
    try {
      await ServersAPI.remove(id);
      addToast(`Server ${address} removed`, 'info');
      await loadServers();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function loadChannels(serverId) {
    try {
      const chs = await ServersAPI.listChannels(serverId);
      channels.update(c => ({ ...c, [serverId]: chs || [] }));
    } catch (e) {
      addToast(e.message, 'error');
      // Ensure the key exists even on failure so the UI doesn't show "Loading..." forever
      channels.update(c => ({ ...c, [serverId]: [] }));
    }
  }

  async function joinChannel(serverId) {
    const input = document.getElementById(`channel-input-${serverId}`);
    let channelName = input?.value.trim();
    if (!channelName) return addToast('Enter a channel name', 'warning');

    // Normalize: channels are case-insensitive per RFC 1459, always lowercase
    channelName = channelName.toLowerCase();
    if (!channelName.startsWith('#')) {
      channelName = '#' + channelName;
    }

    try {
      await ServersAPI.joinChannel(serverId, channelName);
      addToast(`Joined ${channelName}`, 'success');
      input.value = '';
      await loadChannels(serverId);
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function leaveChannel(serverId, channel) {
    try {
      await ServersAPI.leaveChannel(serverId, channel);
      addToast(`Left ${channel}`, 'info');
      await loadChannels(serverId);
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function toggleAutoJoin(serverId, channel, autoJoin) {
    try {
      await ServersAPI.setChannelAutoJoin(serverId, channel, autoJoin);
      addToast(`${channel}: ${autoJoin ? 'Added to' : 'Removed from'} auto-join`, autoJoin ? 'success' : 'info');
      await loadChannels(serverId);
    } catch (e) { addToast(e.message, 'error'); }
  }

  function showTopicModal(topic, channelName) {
    window.__openModal(`Topic: ${channelName}`, `<div style="white-space:pre-wrap;word-wrap:break-word;max-height:400px;overflow-y:auto">${topic || 'No topic set'}</div>`);
  }
</script>

<!-- Connect to Server — always visible at the top -->
<div class="card mb-3">
  <div class="card-header"><span class="card-title">🔌 Connect to Server</span></div>
  <div class="form-row">
    <div class="form-group" style="flex:2">
      <label class="form-label" for="new-address">Server Address</label>
      <input class="form-input" id="new-address" bind:value={newAddress} placeholder="irc.rizon.net" />
    </div>
    <div class="form-group" style="flex:1">
      <label class="form-label" for="new-port">Port</label>
      <input class="form-input" id="new-port" bind:value={newPort} placeholder="6667" type="number" />
    </div>
    <div class="form-group" style="display:flex;align-items:end">
      <button class="btn btn-primary" onclick={connectNewServer}>Connect</button>
    </div>
  </div>
</div>

<!-- Server list section -->
<h3 class="section-title">🖥️ Server List</h3>

{#if loading}
  <div class="spinner"></div>
{:else if sorted.length === 0}
  <div class="empty-state">
    <div class="empty-state-icon">🖥️</div>
    <div class="empty-state-text">No servers configured</div>
    <div class="empty-state-sub">Use the form above to connect to an IRC server</div>
  </div>
{:else}
  {#each sorted as srv}
    <div class="card mb-2">
      <div class="card-header">
        <div>
          <span class="card-title">{srv.address}:{srv.port || 6667}</span>
          <div class="text-sm text-muted mt-1">
            <span class="badge badge-{statusBadge(srv.status).cls}"><span class="badge-dot"></span>{srv.status}</span>
          </div>
        </div>
        <div class="btn-group">
          {#if connectingServers.has(srv.id)}
            <button class="btn btn-sm btn-success" disabled>Connecting...</button>
          {:else if srv.status === 'reconnecting'}
            <button class="btn btn-sm btn-warning" disabled>Reconnecting...</button>
          {:else if srv.status !== 'connected'}
            <button class="btn btn-sm btn-success" onclick={() => connectServer(srv.id)}>Connect</button>
          {:else}
            <button class="btn btn-sm btn-danger" onclick={() => disconnectServer(srv.id)}>Disconnect</button>
          {/if}
          <button class="btn btn-sm btn-ghost" onclick={() => removeServer(srv.id, srv.address)} title="Remove server">🗑️</button>
        </div>
      </div>

      {#if $channels[srv.id] !== undefined}
        {#if $channels[srv.id]?.length}
          <div class="table-container">
            <table>
              <thead><tr><th>Channel</th><th>Topic</th><th>Joined</th><th>Auto-join</th><th>Actions</th></tr></thead>
              <tbody>
                {#each $channels[srv.id] as ch}
                  <tr>
                    <td><strong>{ch.name}</strong></td>
                    <td class="text-muted truncate" style="max-width:300px;cursor:pointer" onclick={() => showTopicModal(ch.topic, ch.name)} title="Click to view full topic">{ch.topic || '—'}</td>
                    <td>
                      <span class="badge" class:badge-ok={ch.joined} class:badge-info={!ch.joined}>
                        {ch.joined ? 'Yes' : 'No'}
                      </span>
                    </td>
                    <td>
                      <span class="badge" class:badge-ok={ch.auto_join} class:badge-info={!ch.auto_join}>
                        {ch.auto_join ? 'Yes' : 'No'}
                      </span>
                    </td>
                    <td>
                      <div class="btn-group">
                        {#if ch.auto_join}
                          <button class="btn btn-sm btn-ghost" onclick={() => toggleAutoJoin(srv.id, ch.name, false)} title="Remove from auto-join">−</button>
                        {:else}
                          <button class="btn btn-sm btn-ghost" onclick={() => toggleAutoJoin(srv.id, ch.name, true)} title="Add to auto-join">+</button>
                        {/if}
                        <button class="btn btn-sm btn-ghost" onclick={() => leaveChannel(srv.id, ch.name)} title="Leave">✕</button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <div class="text-sm text-muted" style="padding:0.5rem 0">No channels configured</div>
        {/if}
      {:else}
        <div class="text-sm text-muted" style="padding:0.5rem 0">Loading channels...</div>
      {/if}

      <div class="flex gap-1 mt-1" style="align-items:center">
        <input class="form-input" id="channel-input-{srv.id}" placeholder="#channel" style="width:200px"
          disabled={srv.status !== 'connected'}
          onkeydown={(e) => e.key === 'Enter' && srv.status === 'connected' && joinChannel(srv.id)} />
        <button class="btn btn-sm btn-primary" disabled={srv.status !== 'connected'} onclick={() => joinChannel(srv.id)}>Join</button>
      </div>
    </div>
  {/each}
{/if}
