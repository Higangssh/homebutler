<script>
  import { onMount } from 'svelte';
  import {
    getInstallable,
    getInstallStatus,
    installApp,
    uninstallApp,
    purgeApp,
  } from './api.js';
  import ConfirmByName from './ConfirmByName.svelte';

  let { canAct = false } = $props();

  // install.App has no json tags on the Go side, so the catalogue arrives with
  // Go field names. Adding tags would change what install_list returns to
  // every MCP caller, which is not this view's decision to make — the same
  // reason WatchCard reads incident.flapping.IsFlapping.
  let apps = $state(null);
  let error = $state('');

  // Status is asked for one app at a time, when its row is opened. Almost
  // every entry in the catalogue is not installed, and asking for all fifteen
  // on every visit would be fifteen requests to answer a question nobody had.
  let openApp = $state('');
  let status = $state(null);
  let statusError = $state('');

  let port = $state('');
  let busy = $state('');
  let actionError = $state('');
  let result = $state(null);
  let purging = $state('');

  onMount(async () => {
    try {
      apps = await getInstallable();
    } catch (err) {
      error = err.message;
    }
  });

  async function open(app) {
    if (openApp === app.Name) {
      openApp = '';
      return;
    }
    openApp = app.Name;
    status = null;
    statusError = '';
    actionError = '';
    result = null;
    purging = '';
    port = app.DefaultPort || '';
    try {
      status = await getInstallStatus(app.Name);
    } catch (err) {
      statusError = err.message;
    }
  }

  async function run(label, work) {
    if (busy) return;
    busy = label;
    actionError = '';
    try {
      return await work();
    } catch (err) {
      actionError = err.message;
    } finally {
      busy = '';
    }
  }

  async function install(app) {
    result = null;
    const answer = await run('install', () => installApp(app.Name, port));
    if (!answer) return;
    result = answer;
    // A refused pre-flight answers 200 with status "failed" and the reasons.
    // It is not a failure of the request: the port is taken, the operator
    // picks another and asks again, so it belongs beside the field.
    if (answer.status !== 'failed') {
      status = await getInstallStatus(app.Name).catch(() => null);
    }
  }

  async function uninstall(app) {
    const answer = await run('uninstall', () => uninstallApp(app.Name));
    if (!answer) return;
    result = answer;
    status = await getInstallStatus(app.Name).catch(() => null);
  }

  async function purge(app) {
    const answer = await run('purge', () => purgeApp(app.Name));
    if (!answer) return;
    result = answer;
    purging = '';
    status = await getInstallStatus(app.Name).catch(() => null);
  }
</script>

<div class="apps">
  {#if error}
    <div class="card"><p class="error">{error}</p></div>
  {:else if !apps}
    <div class="card"><p class="empty">Loading the catalogue…</p></div>
  {:else}
    <div class="card">
      <div class="card-header">
        <h2>Apps</h2>
        <span class="badge">{apps.length}</span>
      </div>
      <p class="lead">
        Each one installs as a Docker compose project under
        <code>~/.homebutler/apps</code>, on a port you choose.
      </p>

      <div class="app-list">
        {#each apps as app}
          <div class="app" class:open={openApp === app.Name}>
            <button class="app-head" onclick={() => open(app)} aria-expanded={openApp === app.Name}>
              <span class="name">{app.Name}</span>
              <span class="detail">{app.Description}</span>
              <span class="port">:{app.DefaultPort}</span>
            </button>

            {#if openApp === app.Name}
              <div class="app-body">
                {#if statusError}
                  <p class="error">{statusError}</p>
                {:else if !status}
                  <p class="empty">Checking…</p>
                {:else if status.installed}
                  <p class="state">Installed · {status.state}</p>
                {:else}
                  <p class="state">Not installed</p>
                {/if}

                {#if canAct && status && !status.installed}
                  <div class="row">
                    <label for="port-{app.Name}">Host port</label>
                    <input id="port-{app.Name}" bind:value={port} disabled={!!busy} />
                    <button onclick={() => install(app)} disabled={!!busy || !port.trim()}>
                      {busy === 'install' ? 'Installing…' : 'Install'}
                    </button>
                  </div>
                  <p class="note">
                    It binds {port || app.DefaultPort} on this host to
                    {app.ContainerPort} in the container. The check runs first and
                    says so if something already has that port.
                  </p>
                {/if}

                {#if result && result.status === 'failed'}
                  <div class="preflight">
                    <strong>Not installed — the check found something first.</strong>
                    <ul>
                      {#each result.issues ?? [] as issue}<li>{issue}</li>{/each}
                    </ul>
                    <span>Change the port above and try again.</span>
                  </div>
                {:else if result && result.status === 'installed'}
                  <p class="ok">Installed on port {result.port} · {result.path}</p>
                {:else if result && result.status === 'uninstalled'}
                  <p class="ok">Stopped and removed. Its data is still on disk.</p>
                {:else if result && result.status === 'purged'}
                  <p class="ok">Removed, with its data.</p>
                {/if}

                {#if canAct && status && status.installed}
                  <div class="row">
                    <button onclick={() => uninstall(app)} disabled={!!busy}>
                      {busy === 'uninstall' ? 'Removing…' : 'Uninstall'}
                    </button>
                    <button
                      class="danger"
                      aria-label="Remove {app.Name} and its data"
                      onclick={() => (purging = purging === app.Name ? '' : app.Name)}
                      disabled={!!busy}
                    >Remove with data</button>
                  </div>
                  <p class="note">
                    Uninstall stops it and leaves its data, so installing it again
                    finds it. Remove with data does not.
                  </p>
                {/if}

                {#if purging === app.Name}
                  <ConfirmByName
                    name={app.Name}
                    what="{app.Name} and everything it has stored will be deleted."
                    busy={busy === 'purge'}
                    onconfirm={() => purge(app)}
                    oncancel={() => (purging = '')}
                  />
                {/if}

                {#if actionError}
                  <p class="error">{actionError}</p>
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
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

  .lead {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin-bottom: 0.75rem;
  }

  .app {
    border-bottom: 1px solid var(--border);
  }

  .app:last-child {
    border-bottom: none;
  }

  .app-head {
    display: flex;
    align-items: baseline;
    gap: 0.625rem;
    width: 100%;
    padding: 0.5rem 0;
    background: none;
    border: none;
    text-align: left;
    cursor: pointer;
    color: inherit;
  }

  .name {
    font-size: 0.8rem;
    font-weight: 500;
    color: var(--text-heading);
    flex-shrink: 0;
  }

  .detail {
    font-size: 0.7rem;
    color: var(--text-secondary);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .port {
    font-size: 0.7rem;
    color: var(--text-secondary);
    flex-shrink: 0;
  }

  .app-body {
    padding: 0 0 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .state {
    font-size: 0.75rem;
    color: var(--text-heading);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  label {
    font-size: 0.7rem;
    color: var(--text-secondary);
  }

  input {
    font-size: 0.75rem;
    padding: 0.25rem 0.5rem;
    width: 6rem;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-primary);
    color: var(--text-heading);
  }

  button {
    font-size: 0.7rem;
    padding: 0.25rem 0.55rem;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-primary);
    color: var(--text-heading);
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .danger {
    border-color: color-mix(in srgb, var(--red) 50%, var(--border));
    color: var(--red);
  }

  .preflight {
    border: 1px solid var(--yellow);
    border-radius: 6px;
    padding: 0.5rem 0.625rem;
    font-size: 0.75rem;
    color: var(--text-heading);
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .preflight ul {
    margin: 0;
    padding-left: 1rem;
  }

  .preflight span {
    font-size: 0.7rem;
    color: var(--text-secondary);
  }

  .note {
    font-size: 0.68rem;
    color: var(--text-secondary);
  }

  .ok {
    font-size: 0.75rem;
    color: var(--green);
  }

  .error {
    font-size: 0.75rem;
    color: var(--red);
  }

  .empty {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }
</style>
