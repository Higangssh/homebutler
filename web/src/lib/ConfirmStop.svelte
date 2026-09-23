<script>
  // The second tier of #242: an action that can be undone, but only by a person
  // going and doing something else.
  //
  // It names what will be down and who has to bring it back, because "are you
  // sure?" is the dialog people learn to click through — and the tier below
  // this one, which asks for a name to be typed, depends on them not having
  // learned that here.
  let { name = '', busy = false, onconfirm, oncancel } = $props();
</script>

<div class="confirm" role="alertdialog" aria-label="Stop {name}">
  <p class="what">
    <strong>{name}</strong> will stop and stay stopped.
  </p>
  <p class="why">
    The dashboard has no way to start it again — you would start it yourself on
    the host.
  </p>
  <div class="actions">
    <button class="cancel" onclick={oncancel} disabled={busy}>Keep it running</button>
    <button class="danger" onclick={onconfirm} disabled={busy}>
      {busy ? 'Stopping…' : 'Stop it'}
    </button>
  </div>
</div>

<style>
  .confirm {
    border: 1px solid var(--red);
    border-radius: 6px;
    padding: 0.625rem 0.75rem;
    margin: 0.5rem 0;
    background: color-mix(in srgb, var(--red) 6%, transparent);
  }

  .what {
    font-size: 0.8rem;
    color: var(--text-heading);
  }

  .why {
    font-size: 0.7rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
    margin-top: 0.625rem;
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
    opacity: 0.6;
    cursor: default;
  }

  .danger {
    border-color: var(--red);
    color: var(--red);
  }
</style>
