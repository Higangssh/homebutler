<script>
  import { onMount, onDestroy } from 'svelte';
  import { getAlerts } from './api.js';

  let { server = '' } = $props();

  let alerts = $state(null);
  let error = $state('');
  let timer;

  async function refresh() {
    try {
      alerts = await getAlerts(server);
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  $effect(() => {
    server;
    alerts = null;
    refresh();
    clearInterval(timer);
    timer = setInterval(refresh, 5000);
  });

  onDestroy(() => clearInterval(timer));

  function statusColor(status) {
    if (status === 'critical') return 'var(--red)';
    if (status === 'warning') return 'var(--yellow)';
    return 'var(--green)';
  }

  function statusLabel(status) {
    if (status === 'critical') return 'CRITICAL';
    if (status === 'warning') return 'WARNING';
    return 'OK';
  }
</script>

<div class="card">
  <div class="card-header">
    <h2>Alerts</h2>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if !alerts}
    <p class="empty">Loading...</p>
  {:else}
    <div class="alert-list">
      <div class="meter">
        <div class="meter-header">
          <span class="meter-label">
            <span class="dot" style="background:{statusColor(alerts.cpu.status)}"></span>
            CPU
          </span>
          <span class="meter-status" style="color:{statusColor(alerts.cpu.status)}">{statusLabel(alerts.cpu.status)}</span>
        </div>
        <div class="bar">
          <div class="bar-fill" style="width:{Math.min(alerts.cpu.current / alerts.cpu.threshold * 100, 100)}%;background:{statusColor(alerts.cpu.status)}"></div>
        </div>
        <span class="meter-value">{alerts.cpu.current}% / {alerts.cpu.threshold}%</span>
      </div>

      <div class="meter">
        <div class="meter-header">
          <span class="meter-label">
            <span class="dot" style="background:{statusColor(alerts.memory.status)}"></span>
            Memory
          </span>
          <span class="meter-status" style="color:{statusColor(alerts.memory.status)}">{statusLabel(alerts.memory.status)}</span>
        </div>
        <div class="bar">
          <div class="bar-fill" style="width:{Math.min(alerts.memory.current / alerts.memory.threshold * 100, 100)}%;background:{statusColor(alerts.memory.status)}"></div>
        </div>
        <span class="meter-value">{alerts.memory.current}% / {alerts.memory.threshold}%</span>
      </div>

      {#each alerts.disks || [] as disk}
        <div class="meter">
          <div class="meter-header">
            <span class="meter-label">
              <span class="dot" style="background:{statusColor(disk.status)}"></span>
              Disk {disk.mount}
            </span>
            <span class="meter-status" style="color:{statusColor(disk.status)}">{statusLabel(disk.status)}</span>
          </div>
          <div class="bar">
            <div class="bar-fill" style="width:{Math.min(disk.current / disk.threshold * 100, 100)}%;background:{statusColor(disk.status)}"></div>
          </div>
          <span class="meter-value">{disk.current}% / {disk.threshold}%</span>
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

  .alert-list {
    display: flex;
    flex-direction: column;
    gap: 0.625rem;
  }

  .meter {
    margin-bottom: 0;
  }

  .meter-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.25rem;
  }

  .meter-label {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    font-size: 0.8rem;
    color: var(--text-heading);
    font-weight: 500;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .meter-status {
    font-weight: 600;
    font-size: 0.7rem;
    text-transform: uppercase;
  }

  .bar {
    height: 6px;
    background: var(--bg-primary);
    border-radius: 3px;
    overflow: hidden;
    margin-bottom: 0.15rem;
  }

  .bar-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.3s ease;
  }

  .meter-value {
    font-size: 0.65rem;
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
