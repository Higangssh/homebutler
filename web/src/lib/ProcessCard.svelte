<script>
  import { onMount, onDestroy } from 'svelte';
  import { getProcesses } from './api.js';

  let { server = '' } = $props();

  let processes = $state([]);
  let error = $state('');
  let timer;

  async function refresh() {
    try {
      processes = await getProcesses(server);
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  $effect(() => {
    server;
    processes = [];
    refresh();
    clearInterval(timer);
    timer = setInterval(refresh, 5000);
  });

  onDestroy(() => clearInterval(timer));
</script>

<div class="card">
  <div class="card-header">
    <h2>Top Processes</h2>
    <span class="badge">by CPU</span>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if processes.length === 0}
    <p class="empty">Loading...</p>
  {:else}
    <table>
      <thead>
        <tr>
          <th class="left">Process</th>
          <th>PID</th>
          <th>CPU</th>
          <th>MEM</th>
        </tr>
      </thead>
      <tbody>
        {#each processes as p}
          <tr>
            <td class="name">{p.name}</td>
            <td class="num">{p.pid}</td>
            <td class="num">{p.cpu.toFixed(1)}%</td>
            <td class="num">{p.mem.toFixed(1)}%</td>
          </tr>
        {/each}
      </tbody>
    </table>
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

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.8rem;
  }

  thead th {
    text-align: right;
    color: var(--text-secondary);
    font-weight: 500;
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding-bottom: 0.375rem;
    border-bottom: 1px solid var(--border);
  }

  th.left {
    text-align: left;
  }

  td {
    padding: 0.3rem 0;
    border-bottom: 1px solid var(--border);
  }

  tr:last-child td {
    border-bottom: none;
  }

  .name {
    color: var(--text-heading);
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .num {
    text-align: right;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
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
