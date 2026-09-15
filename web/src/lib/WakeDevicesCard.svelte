<script>
  import { saveWake, StaleConfigError } from './api.js';

  // Same arrangement as the channels above: the revision is shared with every
  // other form on this screen, so a save hands the new one back rather than
  // keeping a copy that the next save would be refused for.
  let { targets = [], revision = '', editable = false, onsaved, onreload } = $props();

  // Only what has been typed is kept, and the input falls back to what the
  // file holds. Seeding a box per target would mean having one ready for a
  // target that is not on screen yet — and a device added here appears in the
  // list on the very next render, which is where the seeded version broke.
  let edits = $state({});

  function shown(target, field) {
    return edits[target.name]?.[field] ?? target[field] ?? '';
  }

  function typed(name, field, value) {
    edits[name] = { ...(edits[name] ?? {}), [field]: value };
  }

  function changed(target) {
    const pending = edits[target.name];
    if (!pending) return false;
    return Object.entries(pending).some(([field, value]) => value !== (target[field] ?? ''));
  }

  let added = $state({ name: '', mac: '', broadcast: '' });
  let adding = $state(false);
  let confirmRemove = $state('');
  let busy = $state('');
  let message = $state({});
  let notice = $state(null);
  let stale = $state(false);

  // Adding and removing take the card the message would have been in with
  // them, so their answer belongs to the section. Editing keeps its card, and
  // its answer stays next to the field that was changed.
  async function send(key, target, done, atSection) {
    busy = key;
    message = {};
    notice = null;
    stale = false;
    try {
      const result = await saveWake(revision, [target]);
      onsaved?.(result.revision);
      if (atSection) notice = { ok: true, text: done };
      else message = { [key]: { ok: true, text: done } };
      return true;
    } catch (err) {
      if (err instanceof StaleConfigError) stale = true;
      if (atSection) notice = { ok: false, text: err.message };
      else message = { [key]: { ok: false, text: err.message } };
      return false;
    } finally {
      busy = '';
    }
  }

  async function save(event, target) {
    event.preventDefault();
    if (busy) return;
    if (!changed(target)) {
      message = { [target.name]: { ok: false, text: 'Nothing changed.' } };
      return;
    }
    const saved = await send(
      target.name,
      { name: target.name, mac: shown(target, 'mac'), broadcast: shown(target, 'broadcast') },
      'Saved.'
    );
    if (saved) delete edits[target.name];
  }

  async function remove(name) {
    confirmRemove = '';
    await send(name, { name, remove: true }, `Removed ${name}.`, true);
  }

  async function add(event) {
    event.preventDefault();
    if (busy) return;
    if (await send('new', { ...added }, `Added ${added.name}.`, true)) {
      added = { name: '', mac: '', broadcast: '' };
      adding = false;
    }
  }
</script>

<div class="section">
  <div class="section-header">
    <h2>Wake-on-LAN Devices</h2>
    <span class="badge">{targets.length}</span>
    {#if editable && !adding}
      <button type="button" class="add" onclick={() => (adding = true)}>Add a device</button>
    {/if}
  </div>

  {#if notice && notice.ok}
    <p class="saved section-notice">{notice.text}</p>
  {/if}

  {#if stale}
    <div class="stale">
      <p>The config file changed on disk since this page loaded it; reload before saving.</p>
      <button type="button" onclick={() => onreload?.()}>Reload</button>
    </div>
  {/if}

  {#if !editable}
    {#if targets.length === 0}
      <p class="empty">No WoL targets configured</p>
    {:else}
      <div class="wake-list">
        {#each targets as target}
          <div class="wake-item">
            <span class="wake-name">{target.name}</span>
            <div class="wake-details">
              <code class="value">{target.mac}</code>
              {#if target.broadcast}
                <span class="wake-sep">·</span>
                <code class="value">{target.broadcast}</code>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
    <p class="hint read-only">
      Read-only: start <code>homebutler serve</code> with <code>--token</code> to edit devices here.
    </p>
  {:else}
    {#if targets.length === 0 && !adding}
      <p class="empty">No WoL targets configured</p>
    {/if}

    <div class="devices">
      {#each targets as target (target.name)}
        <form class="device" onsubmit={e => save(e, target)}>
          <div class="device-header">
            <span class="wake-name">{target.name}</span>
            {#if confirmRemove === target.name}
              <span class="confirm">
                Remove {target.name}?
                <button type="button" class="danger" onclick={() => remove(target.name)}>Remove</button>
                <button type="button" class="link" onclick={() => (confirmRemove = '')}>Cancel</button>
              </span>
            {:else}
              <button type="button" class="link" onclick={() => (confirmRemove = target.name)}>Remove</button>
            {/if}
          </div>

          <label class="field">
            <span class="label">MAC address</span>
            <input
              name={`${target.name}.mac`}
              autocomplete="off"
              spellcheck="false"
              placeholder="AA:BB:CC:DD:EE:FF"
              value={shown(target, 'mac')}
              oninput={e => typed(target.name, 'mac', e.currentTarget.value)}
            />
          </label>
          <label class="field">
            <span class="label">Broadcast <span class="optional">optional</span></span>
            <input
              name={`${target.name}.broadcast`}
              autocomplete="off"
              spellcheck="false"
              placeholder="255.255.255.255"
              value={shown(target, 'broadcast')}
              oninput={e => typed(target.name, 'broadcast', e.currentTarget.value)}
            />
          </label>

          <div class="actions">
            <button type="submit" disabled={busy === target.name}>
              {busy === target.name ? 'Saving…' : 'Save'}
            </button>
          </div>

          {#if message[target.name]}
            <p class:saved={message[target.name].ok} class:error={!message[target.name].ok}>
              {message[target.name].text}
            </p>
          {/if}
        </form>
      {/each}

      {#if adding}
        <form class="device new" onsubmit={add}>
          <div class="device-header">
            <span class="wake-name">New device</span>
          </div>

          <label class="field">
            <span class="label">Name</span>
            <!-- The name is how the target is addressed, by `homebutler wake`
                 and by the save itself, so it is set once here rather than
                 edited in place: changing it is removing one and adding another. -->
            <input name="new.name" autocomplete="off" required bind:value={added.name} />
          </label>
          <label class="field">
            <span class="label">MAC address</span>
            <input name="new.mac" autocomplete="off" spellcheck="false" required placeholder="AA:BB:CC:DD:EE:FF" bind:value={added.mac} />
          </label>
          <label class="field">
            <span class="label">Broadcast <span class="optional">optional</span></span>
            <input name="new.broadcast" autocomplete="off" spellcheck="false" placeholder="255.255.255.255" bind:value={added.broadcast} />
          </label>

          <div class="actions">
            <button type="submit" disabled={busy === 'new'}>{busy === 'new' ? 'Saving…' : 'Add'}</button>
            <button type="button" class="link" onclick={() => (adding = false)}>Cancel</button>
          </div>

          {#if notice && !notice.ok}
            <p class="error">{notice.text}</p>
          {/if}
        </form>
      {/if}
    </div>
  {/if}
</div>

<style>
  .section {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.25rem;
  }

  h2 {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text-heading);
    margin: 0;
  }

  .badge {
    font-size: 0.75rem;
    color: var(--text-secondary);
    background: var(--bg-primary);
    padding: 0.15rem 0.5rem;
    border-radius: 10px;
  }

  .section-header {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-bottom: 0.75rem;
  }

  .add {
    margin-left: auto;
  }

  .devices {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 0.75rem;
  }

  .device {
    display: flex;
    flex-direction: column;
    gap: 0.55rem;
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem 0.85rem;
    background: var(--bg-secondary);
  }

  .device.new {
    border-style: dashed;
  }

  .device-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .wake-name {
    font-weight: 600;
    font-size: 0.9rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .label {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .optional {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-weight: 400;
  }

  input {
    min-width: 0;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.4rem 0.55rem;
    color: var(--text-primary);
    font-family: inherit;
    font-size: 0.85rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
  }

  .confirm {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.78rem;
    color: var(--text-secondary);
  }

  button {
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

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  button.link {
    background: none;
    color: var(--text-secondary);
    padding: 0.25rem 0.3rem;
    font-weight: 500;
    text-decoration: underline;
  }

  button.danger {
    background: var(--red, #e5534b);
    color: #fff;
  }

  .wake-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .wake-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .wake-details {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .value {
    color: var(--text-primary);
    font-size: 0.8rem;
    overflow-wrap: anywhere;
  }

  .wake-sep {
    color: var(--text-secondary);
  }

  .empty {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin: 0 0 0.5rem;
  }

  .stale {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
    border: 1px solid color-mix(in srgb, var(--yellow) 45%, transparent);
    background: color-mix(in srgb, var(--yellow) 8%, transparent);
    border-radius: 6px;
    padding: 0.5rem 0.7rem;
    font-size: 0.8rem;
  }

  .stale p {
    margin: 0;
  }

  .saved {
    font-size: 0.78rem;
    color: var(--accent);
    margin: 0;
  }

  .section-notice {
    margin-bottom: 0.6rem;
  }

  .error {
    font-size: 0.78rem;
    color: var(--red, #e5534b);
    margin: 0;
  }

  .hint.read-only {
    margin-top: 0.75rem;
    font-size: 0.78rem;
    color: var(--text-secondary);
  }

  @media (max-width: 480px) {
    .devices {
      grid-template-columns: 1fr;
    }

    .wake-item {
      flex-direction: column;
      align-items: flex-start;
      gap: 0.25rem;
    }

    .section-header {
      flex-wrap: wrap;
    }
  }
</style>
