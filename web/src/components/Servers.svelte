<script>
  import { onMount } from 'svelte';
  import { servers, channels } from '../lib/stores.js';
  import { ServersAPI } from '../lib/api.js';
  import { statusBadge } from '../lib/utils.js';
  import { addToast } from '../lib/stores.js';

  let newAddress = 'irc.rizon.net';
  let newPort = 6667;
  let loading = true;

  onMount(async () => {
    await loadServers();
    loading = false;
  });

  async function loadServers() {
    try {
      servers.set(await ServersAPI.list());
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function connectNewServer() {
    if (!newAddress.trim()) return addToast('Enter a server address', 'warning');
    try {
      await ServersAPI.connect({ address: newAddress.trim(), port: newPort });
      addToast(`Connected to ${newAddress}`, 'success');
      await loadServers();
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function connectServer(id) {
    try {
      await ServersAPI.connect(id);
      addToast('Server connected', 'success');
      await loadServers();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function disconnectServer(id) {
    try {
      await ServersAPI.disconnect(id);
      addToast('Server disconnected', 'info');
      await loadServers();
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function loadChannels(serverId) {
    try {
      const chs = await ServersAPI.listChannels(serverId);
      channels.update(c => ({ ...c, [serverId]: chs || [] }));
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function joinChannel(serverId) {
    const input = document.getElementById(`channel-input-${serverId}`);
    let channelName = input?.value.trim();
    if (!channelName) return addToast('Enter a channel name', 'warning');
    
    // Normalize: ensure channel starts with #
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

{#if loading}
  <div class="spinner"></div>
{:else if $servers.length === 0}
  <div class="empty-state">
    <div class="empty-state-icon">🖥️</div>
    <div class="empty-state-text">No servers configured</div>
    <div class="empty-state-sub">Connect to an IRC server to get started</div>
  </div>
{:else}
  {#each $servers as srv}
    <div class="card mb-2">
      <div class="card-header">
        <div>
          <span class="card-title">{srv.address}:{srv.port || 6667}</span>
          <div class="text-sm text-muted mt-1">
            <span class="badge badge-{statusBadge(srv.status).cls}"><span class="badge-dot"></span>{srv.status}</span>
          </div>
        </div>
        <div class="btn-group">
          {#if srv.status !== 'connected'}
            <button class="btn btn-sm btn-success" onclick={() => connectServer(srv.id)}>Connect</button>
          {:else}
            <button class="btn btn-sm btn-danger" onclick={() => disconnectServer(srv.id)}>Disconnect</button>
          {/if}
        </div>
      </div>

      {#await loadChannels(srv.id) then}
        {#if $channels[srv.id]?.length}
          <div class="table-container">
            <table>
              <thead><tr><th>Channel</th><th>Topic</th><th>Auto-join</th><th>Actions</th></tr></thead>
              <tbody>
                {#each $channels[srv.id] as ch}
                  <tr>
                    <td><strong>{ch.name}</strong></td>
                    <td class="text-muted truncate" style="max-width:300px;cursor:pointer" onclick={() => showTopicModal(ch.topic, ch.name)} title="Click to view full topic">{ch.topic || '—'}</td>
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
          <div class="text-sm text-muted" style="padding:0.5rem 0">No channels joined</div>
        {/if}
      {/await}

      <div class="flex gap-1 mt-1" style="align-items:center">
        <input class="form-input" id="channel-input-{srv.id}" placeholder="#channel" style="width:200px" onkeydown={(e) => e.key === 'Enter' && joinChannel(srv.id)} />
        <button class="btn btn-sm btn-primary" onclick={() => joinChannel(srv.id)}>Join</button>
      </div>
    </div>
  {/each}
{/if}

<div class="card mt-2">
  <div class="card-header"><span class="card-title">Connect to Server</span></div>
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
