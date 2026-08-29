<script>
  import { onMount } from 'svelte';
  import { getProxmoxEndpoints, getProxmoxStatus } from './api.js';

  let endpoints = $state(null);
  let selectedEndpoint = $state('');
  let data = $state(null);
  let error = $state('');
  let loading = $state(false);

  onMount(async () => {
    try {
      endpoints = await getProxmoxEndpoints();
      selectedEndpoint = endpoints[0]?.name || '';
    } catch (err) {
      endpoints = [];
      error = err.message;
    }
  });

  $effect(() => {
    const endpoint = selectedEndpoint;
    if (endpoint) refresh(endpoint);
  });

  async function refresh(endpoint) {
    loading = true;
    data = null;
    if (selectedEndpoint === endpoint) error = '';
    try {
      const result = await getProxmoxStatus(endpoint);
      if (selectedEndpoint === endpoint) {
        data = result;
        error = '';
      }
    } catch (err) {
      if (selectedEndpoint === endpoint) error = err.message;
    } finally {
      if (selectedEndpoint === endpoint) loading = false;
    }
  }

  function failed(collector) {
    return data?.failed_collectors?.includes(collector);
  }

  function clusterSummary(entries = []) {
    const summary = { name: 'standalone', quorum: 'n/a', online: 0, total: 0 };
    for (const entry of entries) {
      if (entry.type === 'cluster') {
        summary.name = entry.name || summary.name;
        if (entry.quorate !== undefined) summary.quorum = entry.quorate ? 'yes' : 'no';
      } else if (entry.type === 'node') {
        summary.total++;
        if (entry.online) summary.online++;
      }
    }
    return summary;
  }

  function formatPercent(value) {
    return value == null ? '—' : `${(value * 100).toFixed(1)}%`;
  }

  function formatBytes(value) {
    if (value == null) return '—';
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let size = value;
    let unit = 0;
    while (size >= 1024 && unit < units.length - 1) {
      size /= 1024;
      unit++;
    }
    return `${size.toFixed(unit < 3 ? 0 : 1)} ${units[unit]}`;
  }

  function formatUptime(seconds) {
    if (seconds == null) return '—';
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    return days ? `${days}d ${hours}h` : `${hours}h`;
  }

  function statusClass(status) {
    if (['online', 'running', 'available'].includes(status)) return 'ok';
    if (['offline', 'stopped'].includes(status)) return 'bad';
    return '';
  }
</script>

{#if endpoints === null || endpoints.length > 0 || error}
  <section class="card">
    <div class="card-header">
      <h2>Proxmox VE</h2>
      {#if endpoints?.length === 1}
        <span class="badge">{endpoints[0].name}</span>
      {:else if endpoints?.length > 1}
        <label for="proxmox-endpoint">Endpoint</label>
        <select id="proxmox-endpoint" bind:value={selectedEndpoint}>
          {#each endpoints as endpoint}
            <option value={endpoint.name}>{endpoint.name}</option>
          {/each}
        </select>
      {/if}
    </div>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {:else if endpoints === null || loading || !data}
      <p class="muted">Loading...</p>
    {:else}
      <div class="section">
        <h3>Cluster</h3>
        {#if failed('cluster')}
          <p class="unavailable">Cluster status unavailable</p>
        {:else}
          {@const cluster = clusterSummary(data.cluster)}
          <div class="summary">
            <span><strong>{cluster.name}</strong></span>
            <span>Quorum: {cluster.quorum}</span>
            <span>Nodes: {cluster.online}/{cluster.total} online</span>
          </div>
        {/if}
      </div>

      <div class="section">
        <h3>Nodes</h3>
        {#if failed('resources')}
          <p class="unavailable">Node inventory unavailable</p>
        {:else if !data.resources?.nodes?.length}
          <p class="muted">No nodes found</p>
        {:else}
          <div class="table-wrap">
            <table>
              <thead><tr><th>Name</th><th>Status</th><th>CPU</th><th>Memory</th><th>Uptime</th></tr></thead>
              <tbody>
                {#each data.resources.nodes as node}
                  <tr>
                    <td>{node.name}</td>
                    <td class={statusClass(node.status)}>{node.status}</td>
                    <td>{formatPercent(node.cpu)}</td>
                    <td>{formatBytes(node.mem)} / {formatBytes(node.max_mem)}</td>
                    <td>{formatUptime(node.uptime)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>

      <div class="section">
        <h3>Guests</h3>
        {#if failed('resources')}
          <p class="unavailable">Guest inventory unavailable</p>
        {:else if !data.resources?.guests?.length}
          <p class="muted">No guests found</p>
        {:else}
          <div class="table-wrap">
            <table>
              <thead><tr><th>VMID</th><th>Name</th><th>Type</th><th>Node</th><th>Status</th></tr></thead>
              <tbody>
                {#each data.resources.guests as guest}
                  <tr>
                    <td>{guest.vmid}</td>
                    <td>{guest.name || '—'}</td>
                    <td>{guest.type.toUpperCase()}</td>
                    <td>{guest.node}</td>
                    <td class={statusClass(guest.status)}>{guest.status}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>

      <div class="section">
        <h3>Storage</h3>
        {#if failed('resources')}
          <p class="unavailable">Storage inventory unavailable</p>
        {:else if !data.resources?.storage?.length}
          <p class="muted">No storage found</p>
        {:else}
          <div class="table-wrap">
            <table>
              <thead><tr><th>Name</th><th>Node</th><th>Status</th><th>Used</th></tr></thead>
              <tbody>
                {#each data.resources.storage as store}
                  <tr>
                    <td>{store.name}</td>
                    <td>{store.node || '—'}</td>
                    <td class={statusClass(store.status)}>{store.status || '—'}</td>
                    <td>{formatBytes(store.used)} / {formatBytes(store.total)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>

      {#if data.warnings?.length}
        <div class="warnings" role="status">
          <strong>Warnings</strong>
          <ul>
            {#each data.warnings as warning}<li>{warning}</li>{/each}
          </ul>
        </div>
      {/if}
    {/if}
  </section>
{/if}

<style>
  .card {
    grid-column: 1 / -1;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  .card-header, .summary {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .card-header {
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }

  h2, h3 {
    color: var(--text-heading);
    font-size: 0.875rem;
    font-weight: 600;
  }

  h3 {
    margin-bottom: 0.5rem;
  }

  label, select, .badge {
    font-size: 0.75rem;
  }

  label {
    margin-left: auto;
    color: var(--text-secondary);
  }

  select {
    background: var(--bg-primary);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.35rem 0.6rem;
  }

  .badge {
    color: var(--text-secondary);
    background: var(--bg-primary);
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
  }

  .summary {
    flex-wrap: wrap;
    color: var(--text-secondary);
    font-size: 0.8rem;
  }

  .section {
    border-top: 1px solid var(--border);
    padding-top: 0.75rem;
    margin-top: 0.75rem;
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.78rem;
  }

  th, td {
    text-align: left;
    padding: 0.4rem 0.5rem;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
  }

  th {
    color: var(--text-secondary);
    font-weight: 500;
  }

  .ok { color: var(--green); }
  .bad, .error { color: var(--red); }
  .muted { color: var(--text-secondary); }
  .unavailable, .warnings { color: var(--yellow); }

  .error, .muted, .unavailable, .warnings {
    font-size: 0.8rem;
  }

  .warnings {
    border-top: 1px solid var(--border);
    margin-top: 0.75rem;
    padding-top: 0.75rem;
  }

  .warnings ul {
    margin: 0.25rem 0 0 1.25rem;
  }
</style>
