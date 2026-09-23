<script>
  // The second tier, for a guest. Same shape as ConfirmStop and a different
  // sentence, because what is true of a container is not true of a VM: this
  // screen can start a guest that is off, so the thing to say is that it stays
  // off until somebody does, not that there is no way back.
  let { name = '', vmid = 0, busy = false, onconfirm, oncancel } = $props();
</script>

<div class="confirm" role="alertdialog" aria-label="Shut down {name}">
  <p class="what">
    <strong>{name}</strong> (vmid {vmid}) will shut down and stay off.
  </p>
  <p class="why">
    Proxmox asks the guest to shut down cleanly. Anything running inside it
    stops, and it stays off until it is started again from here or from Proxmox.
  </p>
  <div class="actions">
    <button class="cancel" onclick={oncancel} disabled={busy}>Leave it running</button>
    <button class="danger" onclick={onconfirm} disabled={busy}>
      {busy ? 'Shutting down…' : 'Shut it down'}
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
