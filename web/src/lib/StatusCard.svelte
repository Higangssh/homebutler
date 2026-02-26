<script>
  import { onMount, onDestroy } from 'svelte';
  import { getStatus } from './api.js';

  let { server = '' } = $props();

  let data = $state(null);
  let error = $state('');
  let timer;

  async function refresh() {
    try {
      data = await getStatus(server);
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  $effect(() => {
    server;
    data = null;
    refresh();
    clearInterval(timer);
    timer = setInterval(refresh, 5000);
  });

  onDestroy(() => clearInterval(timer));

  function barColor(pct) {
    if (pct >= 90) return 'var(--red)';
    if (pct >= 70) return 'var(--yellow)';
    return 'var(--green)';
  }
</script>

<div class="card">
  <div class="card-header">
    <h2>System Status</h2>
    {#if data}
      <span class="badge">{data.uptime}</span>
    {/if}
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if !data}
    <p class="loading">Loading...</p>
  {:else}
    <div class="info-row">
      <span class="label">{data.os}/{data.arch}</span>
      <span class="label">{data.cpu.cores} cores</span>
    </div>

    <div class="meter">
      <div class="meter-header">
        <span>CPU</span>
        <span>{data.cpu.usage_percent}%</span>
      </div>
      <div class="bar">
        <div class="bar-fill" style="width:{data.cpu.usage_percent}%;background:{barColor(data.cpu.usage_percent)}"></div>
      </div>
    </div>

    <div class="meter">
      <div class="meter-header">
        <span>Memory</span>
        <span>{data.memory.used_gb} / {data.memory.total_gb} GB ({data.memory.usage_percent}%)</span>
      </div>
      <div class="bar">
        <div class="bar-fill" style="width:{data.memory.usage_percent}%;background:{barColor(data.memory.usage_percent)}"></div>
      </div>
    </div>

    {#each data.disks || [] as disk}
      <div class="meter">
        <div class="meter-header">
          <span>Disk {disk.mount}</span>
          <span>{disk.used_gb} / {disk.total_gb} GB ({disk.usage_percent}%)</span>
        </div>
        <div class="bar">
          <div class="bar-fill" style="width:{disk.usage_percent}%;background:{barColor(disk.usage_percent)}"></div>
        </div>
      </div>
    {/each}
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

  .info-row {
    display: flex;
    gap: 1rem;
    margin-bottom: 0.75rem;
  }

  .label {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .meter {
    margin-bottom: 0.5rem;
  }

  .meter-header {
    display: flex;
    justify-content: space-between;
    font-size: 0.8rem;
    margin-bottom: 0.25rem;
    color: var(--text-secondary);
  }

  .bar {
    height: 6px;
    background: var(--bg-primary);
    border-radius: 3px;
    overflow: hidden;
  }

  .bar-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.3s ease;
  }

  .error {
    color: var(--red);
    font-size: 0.875rem;
  }

  .loading {
    color: var(--text-secondary);
    font-size: 0.875rem;
  }
</style>
