<script>
  import { onDestroy } from 'svelte';
  import { getDocker, restartContainer, stopContainer } from './api.js';
  import ConfirmStop from './ConfirmStop.svelte';

  let { server = '', canAct = false } = $props();

  let containers = $state([]);
  let available = $state(true);
  let message = $state('');
  let error = $state('');
  let timer;

  // The container an action is running against, and the one whose stop is
  // waiting to be confirmed. Names, not indexes: the list refreshes every five
  // seconds and an index points at a different container afterwards.
  let acting = $state('');
  let confirming = $state('');
  let actionError = $state('');

  async function act(name, run) {
    if (acting) return;
    acting = name;
    actionError = '';
    try {
      await run(name, server);
      confirming = '';
      // Docker reports the new state a moment after it accepts the command, so
      // the refresh that matters is the next scheduled one, not this one.
      await refresh();
    } catch (err) {
      actionError = `${name}: ${err.message}`;
    } finally {
      acting = '';
    }
  }

  async function refresh() {
    try {
      const data = await getDocker(server);
      available = data.available ?? true;
      message = data.message ?? '';
      containers = data.containers ?? [];
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  $effect(() => {
    server;
    containers = [];
    confirming = '';
    actionError = '';
    refresh();
    clearInterval(timer);
    timer = setInterval(refresh, 5000);
  });

  onDestroy(() => clearInterval(timer));

  function stateColor(state) {
    if (state === 'running') return 'var(--green)';
    if (state === 'exited') return 'var(--red)';
    return 'var(--yellow)';
  }
</script>

<div class="card">
  <div class="card-header">
    <h2>Docker Containers</h2>
    {#if containers.length > 0}
      <span class="badge">{containers.filter(c => c.state === 'running').length}/{containers.length}</span>
    {/if}
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if !available}
    <p class="unavailable">🐳 Docker is not available</p>
  {:else if containers.length === 0}
    <p class="empty">No containers running</p>
  {:else}
    <div class="container-list">
      {#each containers as c}
        <div class="container-row">
          <span class="dot" style="background:{stateColor(c.state)}"></span>
          <div class="container-info">
            <span class="name">{c.name}</span>
            <span class="detail">{c.image}</span>
          </div>
          <span class="status">{c.status}</span>
          {#if canAct && c.state === 'running'}
            <div class="row-actions">
              <button
                onclick={() => act(c.name, restartContainer)}
                disabled={!!acting}
                aria-label="Restart {c.name}"
                title="Restart {c.name}"
              >{acting === c.name && confirming !== c.name ? '…' : 'Restart'}</button>
              <button
                class="danger"
                onclick={() => (confirming = confirming === c.name ? '' : c.name)}
                disabled={!!acting}
                aria-label="Stop {c.name}"
                title="Stop {c.name}"
              >Stop</button>
            </div>
          {/if}
        </div>
        {#if confirming === c.name}
          <ConfirmStop
            name={c.name}
            busy={acting === c.name}
            onconfirm={() => act(c.name, stopContainer)}
            oncancel={() => (confirming = '')}
          />
        {/if}
      {/each}
    </div>
  {/if}

  {#if actionError}
    <p class="error">{actionError}</p>
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

  .container-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-height: none;
  }

  .container-row {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0.375rem 0;
    border-bottom: 1px solid var(--border);
  }

  .container-row:last-child {
    border-bottom: none;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .container-info {
    flex: 1;
    min-width: 0;
  }

  .name {
    display: block;
    font-size: 0.8rem;
    color: var(--text-heading);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail {
    display: block;
    font-size: 0.7rem;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status {
    font-size: 0.7rem;
    color: var(--text-secondary);
    flex-shrink: 0;
    text-align: right;
  }

  .row-actions {
    display: flex;
    gap: 0.375rem;
    flex-shrink: 0;
  }

  .row-actions button {
    font-size: 0.7rem;
    padding: 0.2rem 0.45rem;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-primary);
    color: var(--text-heading);
    cursor: pointer;
  }

  .row-actions button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .row-actions .danger {
    border-color: color-mix(in srgb, var(--red) 50%, var(--border));
    color: var(--red);
  }

  .error {
    color: var(--red);
    font-size: 0.875rem;
  }

  .empty, .unavailable {
    color: var(--text-secondary);
    font-size: 0.875rem;
  }
</style>
