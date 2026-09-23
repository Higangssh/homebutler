<script>
  import { onMount } from 'svelte';
  import {
    getConfig,
    getServers,
    getServerStatus,
    getVersion,
    clearToken,
    onUnauthorized,
    setToken,
    UnauthorizedError,
  } from './lib/api.js';
  import TokenGate from './lib/TokenGate.svelte';
  import ServerOverviewCard from './lib/ServerOverviewCard.svelte';
  import StatusCard from './lib/StatusCard.svelte';
  import DockerCard from './lib/DockerCard.svelte';
  import ProcessCard from './lib/ProcessCard.svelte';
  import AlertCard from './lib/AlertCard.svelte';
  import PortsCard from './lib/PortsCard.svelte';
  import WakeCard from './lib/WakeCard.svelte';
  import ConfigCard from './lib/ConfigCard.svelte';
  import ReportCard from './lib/ReportCard.svelte';
  import WatchCard from './lib/WatchCard.svelte';
  import ProxmoxCard from './lib/ProxmoxCard.svelte';

  let servers = $state([]);
  let selectedServer = $state('');
  let version = $state('dev');
  let activeTab = $state('dashboard');

  // 'checking' until one request has answered. Nothing that fetches is mounted
  // before then, so a dashboard that needs a token says so once instead of
  // every card reporting its own 401.
  let auth = $state('checking');
  let rejected = $state(false);

  // Whether this dashboard can run an action at all. Without a token the
  // action routes are not registered, so a button would 404 rather than 401 —
  // the screen cannot tell that apart from a missing feature by status code,
  // and finding out by calling is the wrong way round. /api/config already
  // answers it, and reading the same flag the Config tab reads keeps there
  // from being a second copy to disagree with.
  let canAct = $state(false);

  // /api/version is the cheapest guarded endpoint, so it answers the only
  // question that has to be settled first.
  async function checkAuth() {
    try {
      const v = await getVersion();
      version = v.version || 'dev';
      auth = 'ok';
      return true;
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        auth = 'token-required';
        return false;
      }
      // Anything else is a failure the cards can describe better than a
      // full-page message can.
      auth = 'ok';
      return true;
    }
  }

  async function loadCapabilities() {
    try {
      const cfg = await getConfig();
      canAct = !!cfg.editable;
    } catch {
      // A dashboard that cannot answer this is one that shows no actions,
      // which is the safe way to be wrong about it.
      canAct = false;
    }
  }

  async function loadServers() {
    try {
      servers = await getServers();
      const local = servers.find(s => s.local);
      if (local) selectedServer = local.name;
    } catch {}
  }

  async function submitToken(token) {
    setToken(token);
    rejected = false;
    if (await checkAuth()) {
      await Promise.all([loadServers(), loadCapabilities()]);
      return;
    }
    // Never keep a credential the server has refused: the next page load would
    // send it again and land back here with nothing said about why.
    clearToken();
    rejected = true;
  }

  onMount(async () => {
    // A token can stop being valid while the page is open — the server gets
    // restarted under a different one — so any 401 comes back here.
    onUnauthorized(() => {
      auth = 'token-required';
    });
    if (await checkAuth()) await Promise.all([loadServers(), loadCapabilities()]);
  });
</script>

{#if auth === 'checking'}
  <div class="booting"></div>
{:else if auth === 'token-required'}
  <TokenGate {rejected} onsubmit={submitToken} />
{:else}
  <header>
    <div class="header-left">
      <img src="/logo.png" alt="HomeButler" class="logo" />
      <h1>HomeButler</h1>
    </div>
    <nav class="tabs">
      <button
        class="tab"
        class:active={activeTab === 'dashboard'}
        onclick={() => activeTab = 'dashboard'}
      >Dashboard</button>
      <button
        class="tab"
        class:active={activeTab === 'report'}
        onclick={() => activeTab = 'report'}
      >Report</button>
      <button
        class="tab"
        class:active={activeTab === 'watch'}
        onclick={() => activeTab = 'watch'}
      >Watch</button>
      <button
        class="tab"
        class:active={activeTab === 'config'}
        onclick={() => activeTab = 'config'}
      >Config</button>
    </nav>
    {#if activeTab === 'dashboard' && servers.length > 0}
      <div class="header-right">
        <select bind:value={selectedServer}>
          {#each servers as srv}
            <option value={srv.name}>{srv.name}{srv.local ? ' (local)' : ''}</option>
          {/each}
        </select>
      </div>
    {/if}
  </header>

  <main>
    {#if activeTab === 'dashboard'}
      <div class="overview-row">
        <ServerOverviewCard />
      </div>

      <div class="grid">
        <ProxmoxCard />
        <StatusCard server={selectedServer} />
        <DockerCard server={selectedServer} {canAct} />
        <ProcessCard server={selectedServer} />
        <AlertCard server={selectedServer} />
        <PortsCard server={selectedServer} />
        <WakeCard />
      </div>
    {:else if activeTab === 'report'}
      <ReportCard />
    {:else if activeTab === 'watch'}
      <WatchCard />
    {:else}
      <ConfigCard />
    {/if}
  </main>

  <footer>
    <span>homebutler {version} · powered by Go</span>
  </footer>
{/if}

<style>
  .booting {
    min-height: 100vh;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem 1.5rem;
    border-bottom: 1px solid var(--border);
    background: var(--bg-card);
    position: relative;
    gap: 1.5rem;
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .tabs {
    display: flex;
    gap: 0;
  }

  .tab {
    background: none;
    border: none;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 500;
    padding: 0.4rem 0.75rem;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    transition: color 0.15s ease, border-color 0.15s ease;
  }

  .tab:hover {
    color: var(--text-primary);
  }

  .tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  .header-right {
    position: absolute;
    right: 1.5rem;
  }

  select {
    background: var(--bg-primary);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.4rem 0.75rem;
    font-size: 0.875rem;
    cursor: pointer;
    outline: none;
  }

  select:focus {
    border-color: var(--accent);
  }

  .logo {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }

  h1 {
    font-size: 1.25rem;
    font-weight: 600;
    color: var(--accent);
    letter-spacing: -0.01em;
  }

  main {
    max-width: 1600px;
    margin: 0 auto;
    padding: 1.5rem;
  }

  .overview-row {
    margin-bottom: 1rem;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 1rem;
  }

  footer {
    text-align: center;
    padding: 1.5rem;
    color: var(--text-secondary);
    font-size: 0.75rem;
    border-top: 1px solid var(--border);
    margin-top: 2rem;
  }

  @media (max-width: 1024px) {
    .grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  @media (max-width: 640px) {
    .grid {
      grid-template-columns: 1fr;
    }

    header {
      padding: 0.75rem 1rem;
      gap: 0.75rem;
      flex-wrap: wrap;
    }

    /* The server picker is positioned out of the flow so it can sit at the
       right of a wide header. On a phone there is no room for it beside the
       tabs, and being out of the flow it did not push them aside — it covered
       them, so tapping Config opened the picker instead of the settings. */
    .header-right {
      position: static;
      width: 100%;
    }

    .header-right select {
      width: 100%;
    }
  }
</style>
