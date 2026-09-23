<script>
  // The third tier of #242: an action that nothing undoes.
  //
  // A click cannot say which thing the operator meant to lose, so this asks
  // for the name back. That is not friction for its own sake — it is the only
  // input that carries *which one*, and the server refuses anything else:
  // install_purge answers `send confirm_name="uptime-kuma"` and a wrong name
  // is a 400.
  let { name = '', what = '', busy = false, onconfirm, oncancel } = $props();

  let typed = $state('');
  const matches = $derived(typed === name);

  function submit(event) {
    event.preventDefault();
    if (matches && !busy) onconfirm();
  }
</script>

<!-- The dialog role is on the container, not the form: a form is not an
     interactive element and cannot carry one. -->
<div class="confirm" role="alertdialog" aria-label="Permanently remove {name}">
  <form onsubmit={submit}>
    <p class="what">{what}</p>
    <p class="why">Nothing here brings it back.</p>

    <label for="confirm-{name}">Type <strong>{name}</strong> to confirm</label>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      id="confirm-{name}"
      autocomplete="off"
      autofocus
      bind:value={typed}
      placeholder={name}
      disabled={busy}
    />

    <div class="actions">
      <button type="button" class="cancel" onclick={oncancel} disabled={busy}>Keep it</button>
      <button type="submit" class="danger" disabled={!matches || busy}>
        {busy ? 'Removing…' : `Remove ${name} and its data`}
      </button>
    </div>
  </form>
</div>

<style>
  .confirm {
    border: 1px solid var(--red);
    border-radius: 6px;
    padding: 0.75rem;
    margin-top: 0.5rem;
    background: color-mix(in srgb, var(--red) 6%, transparent);
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .what {
    font-size: 0.8rem;
    color: var(--text-heading);
  }

  .why {
    font-size: 0.7rem;
    color: var(--red);
  }

  label {
    font-size: 0.7rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  input {
    font-size: 0.75rem;
    padding: 0.3rem 0.5rem;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-primary);
    color: var(--text-heading);
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
    margin-top: 0.35rem;
  }

  button {
    font-size: 0.75rem;
    padding: 0.3rem 0.625rem;
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
    border-color: var(--red);
    color: var(--red);
  }
</style>
