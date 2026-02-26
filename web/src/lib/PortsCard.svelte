<script>
  import { onMount, onDestroy } from 'svelte';
  import { getPorts } from './api.js';

  let { server = '' } = $props();

  let ports = $state([]);
  let error = $state('');
  let timer;

  async function refresh() {
    try {
      ports = await getPorts(server);
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  $effect(() => {
    server;
    ports = [];
    refresh();
    clearInterval(timer);
    timer = setInterval(refresh, 10000);
  });

  onDestroy(() => clearInterval(timer));
</script>

<div class="card">
  <div class="card-header">
    <h2>Network Ports</h2>
    {#if ports.length > 0}
      <span class="badge">{ports.length} open</span>
    {/if}
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if ports.length === 0}
    <p class="empty">No open ports detected</p>
  {:else}
    <div class="port-list">
      {#each ports as p}
        <div class="port-row">
          <span class="port-num">:{p.port}</span>
          <span class="port-addr">{p.address}</span>
          <span class="port-process">{p.process || '—'}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
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

  .port-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    max-height: none;
  }

  .port-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.375rem 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.8rem;
  }

  .port-row:last-child {
    border-bottom: none;
  }

  .port-num {
    color: var(--accent);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    min-width: 52px;
  }

  .port-addr {
    color: var(--text-secondary);
    flex: 1;
    font-size: 0.75rem;
  }

  .port-process {
    color: var(--text-heading);
    font-weight: 500;
    text-align: right;
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
