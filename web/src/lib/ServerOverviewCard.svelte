<script>
  import { onMount, onDestroy } from 'svelte';
  import { getOverview } from './api.js';

  let servers = $state([]);
  let collectedAt = $state('');
  let error = $state('');
  let timer;

  // One request. The previous version asked for the server list and then
  // fetched each server's status in sequence, so ten machines meant ten SSH
  // round trips in series and one slow host stalled every machine behind it.
  async function refresh() {
    try {
      const overview = await getOverview();
      servers = overview.servers;
      collectedAt = overview.collected_at;
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  onMount(() => {
    refresh();
    timer = setInterval(refresh, 15000);
  });

  onDestroy(() => clearInterval(timer));

  function dotColor(server) {
    if (server.status === 'current') return 'var(--green)';
    if (server.status === 'stale') return 'var(--yellow)';
    return 'var(--red)';
  }

  function age(iso) {
    const seconds = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
    if (seconds < 90) return `${seconds}s ago`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 90) return `${minutes}m ago`;
    return `${Math.round(minutes / 60)}h ago`;
  }
</script>

<div class="card">
  <div class="card-header">
    <h2>Server Overview</h2>
    {#if servers.length > 0}
      <span class="badge">{servers.length} servers</span>
    {/if}
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if servers.length === 0}
    <p class="empty">Loading...</p>
  {:else}
    <div class="server-grid">
      {#each servers as srv}
        <div class="server-item">
          <div class="server-top">
            <span class="dot" style="background:{dotColor(srv)}"></span>
            <span class="server-name">{srv.name}</span>
            <span class="server-type">{srv.local ? 'local' : srv.host}</span>
          </div>

          {#if srv.system}
            <div class="server-metrics">
              <span class="metric">CPU {srv.system.cpu?.usage_percent?.toFixed(0) ?? '—'}%</span>
              <span class="metric">MEM {srv.system.memory?.usage_percent?.toFixed(0) ?? '—'}%</span>
              <span class="metric">{srv.system.uptime ?? '—'}</span>
            </div>
            {#if srv.status === 'stale'}
              <!-- The reading is worth showing and so is its age: a number with
                   no timestamp is what makes stale data look healthy. -->
              <div class="staleness" title={srv.message}>
                last answered {age(srv.updated_at)} · {srv.failure_class}
              </div>
            {/if}
          {:else}
            <div class="server-metrics">
              <span class="metric offline">{srv.failure_class || 'unavailable'}</span>
            </div>
            {#if srv.message}
              <div class="staleness">{srv.message}</div>
            {/if}
            {#if srv.failure_class === 'host_key'}
              <!-- A server added from the settings screen stops here the first
                   time, by design: homebutler will not send a password to a
                   host it has not been told to trust. The fingerprint has to be
                   checked somewhere other than this page for checking it to
                   mean anything, so this says where to go rather than offering
                   a button that would trust whatever answered. -->
              <div class="staleness action">
                Run <code>homebutler trust {srv.name}</code> where homebutler is installed, after checking the
                host key's fingerprint against the machine itself.
              </div>
            {/if}
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .staleness.action {
    color: var(--text-primary);
  }

  .staleness.action code {
    color: var(--accent);
  }

  .staleness {
    margin-top: 0.3rem;
    font-size: 0.7rem;
    line-height: 1.4;
    color: var(--text-secondary);
  }

  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
    transition: border-color 0.2s ease, box-shadow 0.2s ease;
  }

  .card:hover {
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
    box-shadow: 0 0 12px color-mix(in srgb, var(--accent) 10%, transparent);
  }

  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.75rem;
  }

  h2 {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text-heading);
  }

  .badge {
    font-size: 0.75rem;
    color: var(--text-secondary);
    background: var(--bg-primary);
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
  }

  .server-grid {
    display: flex;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .server-item {
    flex: 1;
    min-width: 180px;
    background: var(--bg-primary);
    border-radius: 6px;
    padding: 0.75rem;
    border: 1px solid var(--border);
  }

  .server-top {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .server-name {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text-heading);
  }

  .server-type {
    font-size: 0.7rem;
    color: var(--text-secondary);
    margin-left: auto;
  }

  .server-metrics {
    display: flex;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .metric {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .metric.offline {
    color: var(--red);
  }

  .metric.loading-text {
    color: var(--text-secondary);
    opacity: 0.6;
  }

  .error {
    color: var(--red);
    font-size: 0.875rem;
  }

  .empty {
    color: var(--text-secondary);
    font-size: 0.875rem;
  }
</style>
