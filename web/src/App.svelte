<script>
  import { onMount } from 'svelte';
  import { getServers, getServerStatus } from './lib/api.js';
  import ServerOverviewCard from './lib/ServerOverviewCard.svelte';
  import StatusCard from './lib/StatusCard.svelte';
  import DockerCard from './lib/DockerCard.svelte';
  import ProcessCard from './lib/ProcessCard.svelte';
  import AlertCard from './lib/AlertCard.svelte';
  import PortsCard from './lib/PortsCard.svelte';
  import WakeCard from './lib/WakeCard.svelte';

  let servers = $state([]);
  let selectedServer = $state('');

  onMount(async () => {
    try {
      servers = await getServers();
      const local = servers.find(s => s.local);
      if (local) selectedServer = local.name;
    } catch {}
  });
</script>

<header>
  <div class="header-center">
    <img src="/logo.png" alt="HomeButler" class="logo" />
    <h1>HomeButler</h1>
  </div>
  {#if servers.length > 0}
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
  <div class="overview-row">
    <ServerOverviewCard />
  </div>

  <div class="grid">
    <StatusCard server={selectedServer} />
    <DockerCard server={selectedServer} />
    <ProcessCard server={selectedServer} />
    <AlertCard server={selectedServer} />
    <PortsCard server={selectedServer} />
    <WakeCard />
  </div>
</main>

<footer>
  <span>homebutler v0.5.1 · powered by Go</span>
</footer>

<style>
  header {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem 1.5rem;
    border-bottom: 1px solid var(--border);
    background: var(--bg-card);
    position: relative;
  }

  .header-center {
    display: flex;
    align-items: center;
    gap: 0.5rem;
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
    }
  }
</style>
