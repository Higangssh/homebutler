<script>
  let { rejected = false, onsubmit } = $props();

  let token = $state('');
  let busy = $state(false);

  async function submit(event) {
    event.preventDefault();
    const value = token.trim();
    if (!value || busy) return;
    busy = true;
    try {
      await onsubmit(value);
    } finally {
      busy = false;
    }
  }
</script>

<div class="gate">
  <form class="card" onsubmit={submit}>
    <h1>This dashboard needs a token</h1>
    <p class="lead">
      It was started with <code>--token</code>, so every request has to carry it.
      Paste the token you passed to <code>homebutler serve</code>.
    </p>

    <label for="token">Token</label>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      id="token"
      type="password"
      autocomplete="off"
      autofocus
      bind:value={token}
      placeholder="the value after --token"
      aria-invalid={rejected}
    />

    {#if rejected}
      <p class="error">That token was not accepted. Check it against the one the server was started with.</p>
    {/if}

    <button type="submit" disabled={busy || !token.trim()}>
      {busy ? 'Checking…' : 'Unlock'}
    </button>

    <p class="note">Kept in this browser only. It is never sent anywhere except this dashboard.</p>
  </form>
</div>

<style>
  .gate {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1.5rem;
  }

  .card {
    width: 100%;
    max-width: 26rem;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }

  h1 {
    font-size: 1.05rem;
    font-weight: 600;
    color: var(--text-heading);
    margin: 0;
  }

  .lead {
    font-size: 0.85rem;
    line-height: 1.6;
    color: var(--text-secondary);
    margin: 0 0 0.4rem;
  }

  code {
    color: var(--accent);
    font-size: 0.8rem;
  }

  label {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  input {
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.55rem 0.75rem;
    color: var(--text-primary);
    font-size: 0.875rem;
    font-family: inherit;
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  input[aria-invalid='true'] {
    border-color: var(--red);
  }

  button {
    margin-top: 0.4rem;
    background: var(--accent);
    color: var(--bg-primary);
    border: none;
    border-radius: 6px;
    padding: 0.55rem 0.75rem;
    font-size: 0.875rem;
    font-weight: 600;
    font-family: inherit;
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .error {
    color: var(--red);
    font-size: 0.8rem;
    margin: 0;
  }

  .note {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin: 0.4rem 0 0;
  }
</style>
