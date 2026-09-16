<script>
  import { onMount } from 'svelte';
  import { getReport, saveSnapshot, getDoctor } from './api.js';

  let report = $state(null);
  let doctor = $state(null);
  let error = $state('');
  let doctorError = $state('');
  let saving = $state(false);
  let saved = $state('');

  // Loading this tab does not save a snapshot. Looking is looking: a save
  // moves the window every later comparison is measured from, so it is a
  // button somebody presses.
  async function load() {
    try {
      report = await getReport();
      error = '';
    } catch (err) {
      error = err.message;
    }
  }

  async function loadDoctor() {
    try {
      doctor = await getDoctor();
      doctorError = '';
    } catch (err) {
      doctorError = err.message;
    }
  }

  onMount(() => {
    load();
    loadDoctor();
  });

  async function snapshot() {
    if (saving) return;
    saving = true;
    saved = '';
    try {
      report = await saveSnapshot();
      saved = 'Saved. The next comparison starts from now.';
    } catch (err) {
      error = err.message;
    } finally {
      saving = false;
    }
  }

  // "3 hours ago" rather than a timestamp: the question the line answers is
  // how much of the past the comparison covers.
  function age(stamp) {
    if (!stamp) return '';
    const seconds = Math.max(0, (Date.now() - new Date(stamp).getTime()) / 1000);
    if (seconds < 90) return 'just now';
    if (seconds < 5400) return `${Math.round(seconds / 60)} minutes ago`;
    if (seconds < 172800) return `${Math.round(seconds / 3600)} hours ago`;
    return `${Math.round(seconds / 86400)} days ago`;
  }

  const severityOrder = { fail: 0, warn: 1, pass: 2 };
  const findings = $derived(
    [...(doctor?.findings ?? [])].sort(
      (a, b) => (severityOrder[a.severity] ?? 3) - (severityOrder[b.severity] ?? 3)
    )
  );
</script>

<div class="report-view">
  {#if error}
    <div class="section"><p class="error">{error}</p></div>
  {:else if !report}
    <div class="section"><p class="loading">Reading the machine…</p></div>
  {:else}
    <div class="section">
      <div class="section-header">
        <h2>What changed</h2>
        <!-- The window the comparison covers, stated rather than implied:
             "no changes" means nothing without it. -->
        {#if report.is_baseline}
          <span class="window">first look — nothing to compare against yet</span>
        {:else if report.compared_to}
          <span class="window">compared with the snapshot from {age(report.compared_to)}</span>
        {/if}
        <button type="button" class="snapshot" onclick={snapshot} disabled={saving}>
          {saving ? 'Saving…' : 'Save a snapshot'}
        </button>
      </div>

      {#if saved}
        <p class="saved">{saved}</p>
      {/if}

      {#if report.needs_attention?.length}
        <h3 class="subhead attention">Needs attention</h3>
        <ul class="findings">
          {#each report.needs_attention as item}
            <li>
              <span class="mark">⚠️</span>
              <span class="finding-text">{item.text}</span>
            </li>
          {/each}
        </ul>
      {/if}

      <h3 class="subhead">Notable changes</h3>
      <ul class="changes">
        {#each report.notable_changes ?? [] as change}
          <li class:skipped={change.kind === 'skipped'}>
            <!-- The kind is the word the README documents and --json carries.
                 It is shown as itself rather than translated, so what is on
                 screen is what an agent branches on. -->
            <span class="kind kind-{change.kind}">{change.kind}</span>
            <span class="target">{change.target}</span>
            {#if change.detail}
              <span class="detail">{change.detail}</span>
            {/if}
          </li>
        {/each}
      </ul>

      {#if report.suggested_actions?.length}
        <h3 class="subhead">Suggested actions</h3>
        <ul class="actions">
          {#each report.suggested_actions as action}
            <li>
              <span class="action-text">{action.command ? action.text.split(': ' + action.command)[0] : action.text}</span>
              {#if action.command}
                <code class="command">{action.command}</code>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>

    <div class="section">
      <div class="section-header">
        <h2>Doctor</h2>
        {#if doctor}
          <span class="badge" class:bad={doctor.status === 'fail'}>{doctor.status}</span>
          <span class="window">{doctor.summary.fail} failing · {doctor.summary.warn} to look at · {doctor.summary.pass} fine</span>
        {/if}
      </div>

      {#if doctorError}
        <p class="error">{doctorError}</p>
      {:else if !doctor}
        <p class="loading">Checking…</p>
      {:else}
        <ul class="findings">
          {#each findings as finding}
            <li>
              <span class="severity severity-{finding.severity}">{finding.severity}</span>
              <div class="finding-body">
                <span class="finding-text">{finding.title}</span>
                {#if finding.detail}
                  <span class="detail">{finding.detail}</span>
                {/if}
                {#if finding.command}
                  <!-- A command to copy, not a button. Whether homebutler can
                       carry it out is what runner says, and a button that ran
                       a shell command from a browser is a different product. -->
                  <code class="command">{finding.command}</code>
                  <span class="runner">
                    {#if finding.runner === 'mcp'}
                      an agent can run this ({finding.tool})
                    {:else if finding.runner === 'cli'}
                      run this where homebutler is installed
                    {:else}
                      run this on the machine
                    {/if}
                  </span>
                {/if}
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</div>

<style>
  .report-view {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .section {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  .section-header {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.6rem;
    margin-bottom: 0.75rem;
  }

  h2 {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text-heading);
    margin: 0;
  }

  .subhead {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    margin: 1rem 0 0.4rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .subhead:first-of-type {
    margin-top: 0;
  }

  .subhead.attention {
    color: var(--yellow, #d4a72c);
  }

  .window {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .badge {
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-secondary);
    background: var(--bg-primary);
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
  }

  .badge.bad {
    color: var(--red, #e5534b);
  }

  .snapshot {
    margin-left: auto;
    background: var(--accent);
    color: var(--bg-primary);
    border: none;
    border-radius: 6px;
    padding: 0.35rem 0.8rem;
    font-family: inherit;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
  }

  .snapshot:disabled {
    opacity: 0.5;
    cursor: default;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .changes li {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 0.5rem;
    font-size: 0.8rem;
  }

  .changes li.skipped {
    opacity: 0.75;
  }

  /* The kind column is fixed width so the words line up the way they do in a
     terminal — the shape is what makes a list of changes scannable. */
  .kind {
    flex: 0 0 auto;
    min-width: 4.5rem;
    font-size: 0.7rem;
    font-weight: 600;
    letter-spacing: 0.03em;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    text-align: center;
  }

  .kind-replaced,
  .kind-gone {
    color: var(--red, #e5534b);
    border-color: color-mix(in srgb, var(--red, #e5534b) 45%, transparent);
  }

  .kind-new,
  .kind-state {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }

  .kind-port,
  .kind-disk,
  .kind-image {
    color: var(--yellow, #d4a72c);
    border-color: color-mix(in srgb, var(--yellow, #d4a72c) 45%, transparent);
  }

  .target {
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .detail,
  .runner {
    color: var(--text-secondary);
    font-size: 0.75rem;
    overflow-wrap: anywhere;
  }

  .findings li {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    font-size: 0.8rem;
  }

  .finding-body {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
  }

  .finding-text {
    overflow-wrap: anywhere;
  }

  .severity {
    flex: 0 0 auto;
    min-width: 3.2rem;
    font-size: 0.68rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    text-align: center;
    padding: 0.1rem 0.35rem;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
  }

  .severity-fail {
    color: var(--red, #e5534b);
    border-color: color-mix(in srgb, var(--red, #e5534b) 45%, transparent);
  }

  .severity-warn {
    color: var(--yellow, #d4a72c);
    border-color: color-mix(in srgb, var(--yellow, #d4a72c) 45%, transparent);
  }

  .severity-pass {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
  }

  .actions li {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    font-size: 0.8rem;
  }

  .command {
    font-size: 0.75rem;
    color: var(--accent);
    background: var(--bg-primary);
    border-radius: 4px;
    padding: 0.15rem 0.4rem;
    overflow-wrap: anywhere;
    /* Selectable on a phone, because copying it is the point. */
    user-select: all;
  }

  .loading,
  .error,
  .saved {
    font-size: 0.8rem;
    margin: 0;
  }

  .loading {
    color: var(--text-secondary);
  }

  .error {
    color: var(--red, #e5534b);
  }

  .saved {
    color: var(--accent);
    margin-bottom: 0.5rem;
  }

  @media (max-width: 480px) {
    .section-header {
      flex-wrap: wrap;
    }

    .snapshot {
      margin-left: 0;
    }
  }
</style>
