<script>
  import { onMount } from 'svelte';
  import { getWatch, getWatchIncidents, getWatchIncident } from './api.js';

  let overview = $state(null);
  let incidents = $state(null);
  let error = $state('');

  let openId = $state('');
  let openIncident = $state(null);
  let openError = $state('');

  onMount(async () => {
    try {
      [overview, incidents] = await Promise.all([getWatch(), getWatchIncidents(25)]);
    } catch (err) {
      error = err.message;
    }
  });

  // The list has no logs in it on purpose, so opening one is a second request.
  async function toggle(id) {
    if (openId === id) {
      openId = '';
      return;
    }
    openId = id;
    openIncident = null;
    openError = '';
    try {
      openIncident = await getWatchIncident(id);
    } catch (err) {
      openError = err.message;
    }
  }

  function when(value) {
    const at = new Date(value);
    const minutes = Math.round((Date.now() - at.getTime()) / 60000);
    if (minutes < 1) return 'just now';
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 48) return `${hours}h ago`;
    return `${Math.round(hours / 24)}d ago`;
  }

  function exact(value) {
    return new Date(value).toLocaleString();
  }

  // FlappingResult has no json tags on the Go side, so it arrives with Go field
  // names. Adding tags would change what `watch history --json` and the MCP
  // tool return, which is not this view's decision to make.
  function flapping(incident) {
    return incident.flapping?.IsFlapping ? incident.flapping : null;
  }
</script>

<div class="watch-view">
  {#if error}
    <div class="card"><p class="error">{error}</p></div>
  {:else if !overview}
    <div class="card"><p class="empty">Loading watch state...</p></div>
  {:else}
    {#if overview.targets.length > 0 && !overview.service.installed}
      <!-- The same state doctor warns about, in the same words: a list with
           entries and nothing installed to poll it records no incidents and
           sends no notifications. -->
      <div class="banner warn">
        <strong>{overview.targets.length} target(s) on the watch list and no service installed to check them</strong>
        <span>Nothing is polling them, so a restart records no incident and sends no notification.</span>
        <code>homebutler watch install</code>
      </div>
    {:else if overview.targets.length > 0}
      <div class="banner ok">
        <strong>{overview.targets.length} target(s) watched by an installed service</strong>
        <span>Unit: <code class="inline">{overview.service.unit}</code></span>
      </div>
    {/if}

    <div class="section">
      <div class="section-header">
        <h2>Watched</h2>
        <span class="badge">{overview.targets.length}</span>
      </div>
      {#if overview.targets.length === 0}
        <p class="empty">
          Nothing is being watched. <code class="inline">homebutler watch add &lt;container&gt;</code>
        </p>
      {:else}
        <div class="target-grid">
          {#each overview.targets as target}
            <div class="mini-card">
              <div class="mini-head">
                <span class="name">{target.container}</span>
                <span class="kind">{target.kind || 'docker'}</span>
              </div>
              {#if target.unit && target.unit !== target.container}
                <code class="unit">{target.unit}</code>
              {/if}
              <span class="since">watched since {exact(target.added_at)}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="section">
      <div class="section-header">
        <h2>Incidents</h2>
        <span class="badge">
          {#if overview.retention.max > 0}
            {overview.retention.kept} of {overview.retention.max} kept
          {:else}
            {overview.retention.kept} kept · unlimited
          {/if}
        </span>
      </div>

      {#if !incidents}
        <p class="empty">Loading incidents...</p>
      {:else if incidents.length === 0}
        <p class="empty">No incidents recorded. Nothing being watched has restarted.</p>
      {:else}
        <div class="incidents">
          {#each incidents as incident}
            <div class="incident" class:open={openId === incident.id}>
              <button class="incident-head" onclick={() => toggle(incident.id)}>
                <span class="container">{incident.container}</span>
                <span class="tags">
                  {#if flapping(incident)}
                    <span class="tag flap">flapping · {incident.flapping.Count} in {incident.flapping.Window}</span>
                  {/if}
                  {#if incident.oom_killed}
                    <span class="tag oom">OOM killed</span>
                  {/if}
                  {#if incident.exit_code !== undefined && incident.exit_code !== null}
                    <span class="tag">exit {incident.exit_code}</span>
                  {/if}
                  <span class="tag">restart #{incident.restart_count}</span>
                </span>
                <span class="when" title={exact(incident.detected_at)}>{when(incident.detected_at)}</span>
              </button>

              {#if openId === incident.id}
                <div class="detail">
                  {#if openError}
                    <p class="error">{openError}</p>
                  {:else if !openIncident}
                    <p class="empty">Loading logs...</p>
                  {:else}
                    <div class="logs">
                      <div class="log-block">
                        <span class="log-label">Before the restart</span>
                        <pre>{openIncident.pre_logs || '(nothing captured)'}</pre>
                      </div>
                      <div class="log-block">
                        <span class="log-label">After</span>
                        <pre>{openIncident.post_logs || '(nothing captured)'}</pre>
                      </div>
                    </div>
                  {/if}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <p class="hint">Recorded by <code class="inline">homebutler watch</code> on this machine.</p>
  {/if}
</div>

<style>
  .watch-view {
    max-width: 900px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .card,
  .section {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  .banner {
    border-radius: 8px;
    padding: 0.85rem 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    border: 1px solid var(--border);
    background: var(--bg-card);
  }

  .banner.warn {
    border-color: color-mix(in srgb, var(--yellow) 45%, transparent);
    background: color-mix(in srgb, var(--yellow) 8%, var(--bg-card));
  }

  .banner.ok {
    border-color: color-mix(in srgb, var(--green) 35%, transparent);
    background: color-mix(in srgb, var(--green) 6%, var(--bg-card));
  }

  .banner strong {
    font-size: 0.875rem;
    color: var(--text-heading);
  }

  .banner span {
    font-size: 0.8rem;
    color: var(--text-secondary);
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

  .badge {
    font-size: 0.75rem;
    color: var(--text-secondary);
    background: var(--bg-primary);
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
  }

  .target-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 0.75rem;
  }

  .mini-card {
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.7rem 0.9rem;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .mini-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .name {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-heading);
  }

  .kind {
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    padding: 0.1rem 0.4rem;
    border-radius: 8px;
  }

  .unit,
  .inline {
    font-size: 0.75rem;
    color: var(--text-primary);
    font-family: monospace;
  }

  .since {
    font-size: 0.7rem;
    color: var(--text-secondary);
  }

  .incidents {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .incident {
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    overflow: hidden;
  }

  .incident.open {
    border-color: var(--accent);
  }

  .incident-head {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.6rem 0.85rem;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    font-family: inherit;
  }

  .container {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-heading);
    min-width: 7rem;
  }

  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
    flex: 1;
  }

  .tag {
    font-size: 0.68rem;
    color: var(--text-secondary);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.1rem 0.4rem;
  }

  .tag.flap {
    color: var(--yellow);
    border-color: color-mix(in srgb, var(--yellow) 40%, transparent);
  }

  .tag.oom {
    color: var(--red);
    border-color: color-mix(in srgb, var(--red) 40%, transparent);
  }

  .when {
    font-size: 0.75rem;
    color: var(--text-secondary);
    white-space: nowrap;
  }

  .detail {
    border-top: 1px solid var(--border);
    padding: 0.75rem 0.85rem;
  }

  .logs {
    display: grid;
    gap: 0.75rem;
  }

  .log-label {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-secondary);
  }

  pre {
    margin: 0.3rem 0 0;
    padding: 0.6rem 0.75rem;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 0.75rem;
    line-height: 1.6;
    color: var(--text-primary);
    overflow-x: auto;
    white-space: pre;
  }

  .hint {
    text-align: center;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  .error {
    color: var(--red);
    font-size: 0.875rem;
  }

  .empty {
    color: var(--text-secondary);
    font-size: 0.875rem;
  }

  @media (max-width: 640px) {
    .incident-head {
      flex-wrap: wrap;
    }
  }
</style>
