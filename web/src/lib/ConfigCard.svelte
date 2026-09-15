<script>
  import { onMount } from 'svelte';
  import { getConfig, saveAlerts, StaleConfigError } from './api.js';
  import NotifyCard from './NotifyCard.svelte';
  import WakeDevicesCard from './WakeDevicesCard.svelte';
  import ServersCard from './ServersCard.svelte';
  import ProxmoxSettingsCard from './ProxmoxSettingsCard.svelte';

  let data = $state(null);
  let error = $state('');

  // Editing state. Kept beside the loaded data rather than in it, so a failed
  // save leaves what the person typed on screen instead of reverting it.
  let draft = $state({ cpu: '', memory: '', disk: '' });
  let saving = $state(false);
  let saved = $state('');
  let saveError = $state('');
  let stale = $state(false);

  async function load() {
    try {
      data = await getConfig();
      draft = {
        cpu: String(data.alerts.cpu ?? ''),
        memory: String(data.alerts.memory ?? ''),
        disk: String(data.alerts.disk ?? ''),
      };
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  onMount(load);

  function changedThresholds() {
    const out = {};
    for (const key of ['cpu', 'memory', 'disk']) {
      const value = Number(draft[key]);
      if (draft[key] !== '' && Number.isFinite(value) && value !== data.alerts[key]) {
        out[key] = value;
      }
    }
    return out;
  }

  async function submitThresholds(event) {
    event.preventDefault();
    const changed = changedThresholds();
    if (Object.keys(changed).length === 0 || saving) return;

    saving = true;
    saveError = '';
    saved = '';
    stale = false;
    try {
      const result = await saveAlerts(data.revision, changed);
      data = { ...data, alerts: { ...data.alerts, ...changed }, revision: result.revision };
      saved = result.restart_needed?.length
        ? `Saved. ${result.restart_needed.join(' and ')} pick this up when they next start.`
        : 'Saved.';
    } catch (err) {
      if (err instanceof StaleConfigError) {
        // Not a failed request: the file moved on. Reloading is the answer,
        // and doing it silently would throw away what they typed.
        stale = true;
        saveError = err.message;
      } else {
        saveError = err.message;
      }
    } finally {
      saving = false;
    }
  }

  // A notify save rewrites the same file the thresholds form holds a revision
  // for, so the new revision has to reach this component. Only the notify half
  // is refreshed: reloading everything would throw away a threshold someone
  // was halfway through typing.
  async function notifySaved() {
    await refreshSection('notify');
  }

  async function wakeSaved() {
    await refreshSection('wake');
  }

  async function serversSaved() {
    await refreshSection('servers');
  }

  async function proxmoxSaved() {
    await refreshSection('proxmox');
  }

  // One section is refreshed at a time. Reloading everything would throw away
  // a threshold someone is halfway through typing on the same screen.
  async function refreshSection(name) {
    try {
      const fresh = await getConfig();
      data = { ...data, [name]: fresh[name], revision: fresh.revision ?? data.revision };
    } catch (err) {
      saveError = err.message;
    }
  }

  async function reload() {
    stale = false;
    saveError = '';
    saved = '';
    await load();
  }
</script>

<div class="config-view">
  {#if error}
    <div class="card">
      <p class="error">{error}</p>
    </div>
  {:else if !data}
    <div class="card">
      <p class="loading">Loading config...</p>
    </div>
  {:else}
    <div class="config-path">
      <span class="path-label">Config file</span>
      <code class="path-value">{data.path}</code>
    </div>

    <ServersCard
      servers={data.servers ?? []}
      revision={data.revision ?? ''}
      editable={data.editable}
      onsaved={serversSaved}
      onreload={reload}
    />

    <!-- Alert Thresholds -->
    <div class="section">
      <div class="section-header">
        <h2>Alert Thresholds</h2>
      </div>

      {#if data.transport_warning}
        <p class="transport">{data.transport_warning}</p>
      {/if}

      {#if !data.editable}
        <div class="thresholds">
          <div class="threshold-item">
            <span class="label">CPU</span><code class="value">{data.alerts.cpu}%</code>
          </div>
          <div class="threshold-item">
            <span class="label">Memory</span><code class="value">{data.alerts.memory}%</code>
          </div>
          <div class="threshold-item">
            <span class="label">Disk</span><code class="value">{data.alerts.disk}%</code>
          </div>
        </div>
        <p class="hint read-only">
          Read-only: start <code>homebutler serve</code> with <code>--token</code> to edit settings here.
        </p>
      {:else}
        <form class="thresholds editable" onsubmit={submitThresholds}>
          <label class="threshold-item">
            <span class="label">CPU</span>
            <input type="number" min="1" max="100" step="0.5" bind:value={draft.cpu} />
            <span class="unit">%</span>
          </label>
          <label class="threshold-item">
            <span class="label">Memory</span>
            <input type="number" min="1" max="100" step="0.5" bind:value={draft.memory} />
            <span class="unit">%</span>
          </label>
          <label class="threshold-item">
            <span class="label">Disk</span>
            <input type="number" min="1" max="100" step="0.5" bind:value={draft.disk} />
            <span class="unit">%</span>
          </label>
          <button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
        </form>

        {#if stale}
          <div class="stale">
            <p>{saveError}</p>
            <button type="button" onclick={reload}>Reload</button>
          </div>
        {:else if saveError}
          <p class="error">{saveError}</p>
        {:else if saved}
          <p class="saved">{saved}</p>
        {/if}
      {/if}
    </div>

    <ProxmoxSettingsCard
      endpoints={data.proxmox ?? []}
      revision={data.revision ?? ''}
      editable={data.editable}
      onsaved={proxmoxSaved}
      onreload={reload}
    />

    <NotifyCard
      channels={data.notify ?? []}
      revision={data.revision ?? ''}
      editable={data.editable}
      onsaved={notifySaved}
      onreload={reload}
    />

    <WakeDevicesCard
      targets={data.wake ?? []}
      revision={data.revision ?? ''}
      editable={data.editable}
      onsaved={wakeSaved}
      onreload={reload}
    />

    <div class="hint">
      Edit via CLI: <code>homebutler init</code>
    </div>
  {/if}
</div>

<style>
  .transport {
    font-size: 0.8rem;
    line-height: 1.5;
    color: var(--yellow);
    border: 1px solid color-mix(in srgb, var(--yellow) 40%, transparent);
    background: color-mix(in srgb, var(--yellow) 8%, transparent);
    border-radius: 6px;
    padding: 0.5rem 0.7rem;
    margin-bottom: 0.75rem;
  }

  .thresholds.editable {
    align-items: flex-end;
    gap: 1rem;
    /* Three inputs and a button do not fit across a phone. Without wrapping
       they did not shrink either: they made the whole page wider than the
       screen, and every card on it scrolled sideways. */
    flex-wrap: wrap;
  }

  .thresholds.editable .threshold-item {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.25rem;
  }

  input[type='number'] {
    width: 6rem;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.35rem 0.5rem;
    color: var(--text-primary);
    font-family: inherit;
    font-size: 0.85rem;
  }

  .unit {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  button {
    background: var(--accent);
    color: var(--bg-primary);
    border: none;
    border-radius: 6px;
    padding: 0.4rem 0.9rem;
    font-family: inherit;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .stale {
    margin-top: 0.6rem;
    display: flex;
    align-items: center;
    gap: 0.75rem;
    border: 1px solid color-mix(in srgb, var(--yellow) 45%, transparent);
    background: color-mix(in srgb, var(--yellow) 8%, transparent);
    border-radius: 6px;
    padding: 0.5rem 0.7rem;
  }

  .stale p {
    font-size: 0.8rem;
    color: var(--text-primary);
    margin: 0;
  }

  .saved {
    margin-top: 0.6rem;
    font-size: 0.8rem;
    color: var(--green);
  }

  .read-only {
    text-align: left;
  }

  .config-view {
    max-width: 900px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  .config-path {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem 1.25rem;
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .path-label {
    font-size: 0.8rem;
    color: var(--text-secondary);
    white-space: nowrap;
  }

  .path-value {
    font-size: 0.85rem;
    color: var(--accent);
    /* A path has no spaces to break at, so on a phone it set the width of the
       whole screen and every card below it scrolled sideways. */
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .section {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  .section-header {
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









  .label {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .value {
    font-size: 0.8rem;
    color: var(--text-primary);
    font-family: monospace;
  }

  .thresholds {
    display: flex;
    gap: 2rem;
    flex-wrap: wrap;
  }

  .threshold-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }







  .hint {
    text-align: center;
    font-size: 0.8rem;
    color: var(--text-secondary);
    padding: 0.5rem 0;
  }

  .hint code {
    color: var(--accent);
    font-size: 0.8rem;
  }

  .error {
    color: var(--red);
    font-size: 0.875rem;
  }

  .loading {
    color: var(--text-secondary);
    font-size: 0.875rem;
  }


  @media (max-width: 640px) {

    .thresholds {
      flex-direction: column;
      gap: 0.5rem;
    }

    /* flex-end lines the inputs up with the Save button while this is a row.
       Once it is a column the same rule means "push it all to the right", and
       that is where the thresholds sat on a phone. */
    .thresholds.editable {
      align-items: stretch;
    }
  }
</style>
