<script>
  import { saveProxmox, StaleConfigError } from './api.js';

  let { endpoints = [], revision = '', editable = false, onsaved, onreload } = $props();

  let edits = $state({});
  let clearing = $state({});
  let added = $state(null);
  let confirmRemove = $state('');
  let busy = $state('');
  let message = $state({});
  let notice = $state(null);
  let stale = $state(false);

  function shown(endpoint, field, fallback = '') {
    return edits[endpoint.name]?.[field] ?? endpoint[field] ?? fallback;
  }

  // A port of 0 means the file does not set one, so the box is empty and the
  // placeholder says what will be used. Showing 0 made the form fail the
  // browser's own min=1 check, and a form that will not submit says nothing
  // about why.
  function portValue(item) {
    const pending = edits[item.name]?.port;
    return pending !== undefined ? pending : item.port ? String(item.port) : '';
  }

  function typed(name, field, value) {
    edits[name] = { ...(edits[name] ?? {}), [field]: value };
  }

  function isCleared(name, field) {
    return Boolean(clearing[`${name}.${field}`]);
  }

  function toggleClear(name, field) {
    const key = `${name}.${field}`;
    clearing[key] = !clearing[key];
  }

  function patchFor(endpoint) {
    const patch = { name: endpoint.name };
    const pending = edits[endpoint.name] ?? {};

    for (const field of ['host', 'token_id', 'action_token_id']) {
      if (pending[field] !== undefined && pending[field] !== (endpoint[field] ?? '')) {
        patch[field] = pending[field];
      }
    }
    if (pending.port !== undefined && Number(pending.port) !== (endpoint.port ?? 0)) {
      patch.port = Number(pending.port);
    }
    if (pending.rename !== undefined && pending.rename !== endpoint.name && pending.rename !== '') {
      patch.rename = pending.rename;
    }

    for (const [field, key] of [['token', 'token'], ['action_token', 'action_token']]) {
      if (isCleared(endpoint.name, field)) patch[key] = { clear: true };
      else if (pending[field]) patch[key] = { value: pending[field] };
    }

    return patch;
  }

  async function send(key, list, done, atSection) {
    busy = key;
    message = {};
    notice = null;
    stale = false;
    try {
      const result = await saveProxmox(revision, list);
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

  async function save(event, endpoint) {
    event.preventDefault();
    if (busy) return;
    const patch = patchFor(endpoint);
    if (Object.keys(patch).length <= 1) {
      message = { [endpoint.name]: { ok: false, text: 'Nothing changed.' } };
      return;
    }
    if (await send(endpoint.name, [patch], 'Saved.')) {
      delete edits[endpoint.name];
      for (const key of Object.keys(clearing)) {
        if (key.startsWith(`${endpoint.name}.`)) clearing[key] = false;
      }
    }
  }

  async function remove(name) {
    confirmRemove = '';
    await send(name, [{ name, remove: true }], `Removed ${name}.`, true);
  }

  async function add(event) {
    event.preventDefault();
    if (busy) return;
    const endpoint = { name: added.name, host: added.host, token_id: added.token_id };
    if (added.port) endpoint.port = Number(added.port);
    if (added.token) endpoint.token = { value: added.token };
    if (await send('new', [endpoint], `Added ${added.name}.`, true)) added = null;
  }
</script>

<div class="section">
  <div class="section-header">
    <h2>Proxmox</h2>
    <span class="badge">{endpoints.length}</span>
    {#if editable && !added}
      <button type="button" class="add" onclick={() => (added = { name: '', host: '', port: '', token_id: '', token: '' })}>
        Add an endpoint
      </button>
    {/if}
  </div>

  {#if notice}
    <p class="section-notice" class:saved={notice.ok} class:error={!notice.ok}>{notice.text}</p>
  {/if}

  {#if stale}
    <div class="stale">
      <p>The config file changed on disk since this page loaded it; reload before saving.</p>
      <button type="button" onclick={() => onreload?.()}>Reload</button>
    </div>
  {/if}

  {#if endpoints.length === 0 && !added}
    <p class="empty">No Proxmox endpoint configured</p>
  {/if}

  <div class="endpoints">
    {#each endpoints as endpoint (endpoint.name)}
      <div class="endpoint">
        <div class="endpoint-header">
          <span class="endpoint-name">{endpoint.name}</span>
        </div>

        {#if !editable}
          <div class="rows">
            <div class="row"><span class="label">Host</span><code class="value">{endpoint.host}</code></div>
            <div class="row"><span class="label">Token id</span><code class="value">{endpoint.token_id || '—'}</code></div>
            <div class="row">
              <span class="label">Token</span>
              <span class="state" class:on={endpoint.token_set}>{endpoint.token_set ? 'set' : 'not set'}</span>
            </div>
          </div>
        {:else}
          <form class="fields" onsubmit={e => save(e, endpoint)}>
            <label class="field">
              <span class="label">Name</span>
              <input
                name={`${endpoint.name}.rename`}
                autocomplete="off"
                value={shown(endpoint, 'rename', endpoint.name)}
                oninput={e => typed(endpoint.name, 'rename', e.currentTarget.value)}
              />
            </label>
            <div class="pair">
              <label class="field">
                <span class="label">Host</span>
                <input
                  name={`${endpoint.name}.host`}
                  autocomplete="off"
                  spellcheck="false"
                  value={shown(endpoint, 'host')}
                  oninput={e => typed(endpoint.name, 'host', e.currentTarget.value)}
                />
              </label>
              <label class="field port">
                <span class="label">Port</span>
                <input
                  name={`${endpoint.name}.port`}
                  type="number"
                  min="1"
                  max="65535"
                  placeholder="8006"
                  value={portValue(endpoint)}
                  oninput={e => typed(endpoint.name, 'port', e.currentTarget.value)}
                />
              </label>
            </div>

            <label class="field">
              <span class="label">Token id</span>
              <input
                name={`${endpoint.name}.token_id`}
                autocomplete="off"
                spellcheck="false"
                placeholder="root@pam!homebutler"
                value={shown(endpoint, 'token_id')}
                oninput={e => typed(endpoint.name, 'token_id', e.currentTarget.value)}
              />
            </label>

            {#each [{ field: 'token', label: 'Token', set: endpoint.token_set, inFile: endpoint.token_in_file }, { field: 'action_token', label: 'Action token', set: endpoint.action_token_set, inFile: endpoint.action_token_in_file }] as credential}
              <label class="field">
                <span class="label">
                  {credential.label}
                  <span class="state" class:on={credential.set}>{credential.set ? 'set' : 'not set'}</span>
                </span>
                {#if credential.inFile}
                  <p class="hint">Read from a file, so it is changed in the config rather than here.</p>
                {:else}
                  <div class="control">
                    <input
                      name={`${endpoint.name}.${credential.field}`}
                      type="password"
                      autocomplete="off"
                      disabled={isCleared(endpoint.name, credential.field)}
                      placeholder={credential.set ? 'leave blank to keep' : 'not set'}
                      value={edits[endpoint.name]?.[credential.field] ?? ''}
                      oninput={e => typed(endpoint.name, credential.field, e.currentTarget.value)}
                    />
                    {#if credential.set}
                      <button type="button" class="link" onclick={() => toggleClear(endpoint.name, credential.field)}>
                        {isCleared(endpoint.name, credential.field) ? 'Keep' : 'Clear'}
                      </button>
                    {/if}
                  </div>
                {/if}
              </label>
            {/each}

            <!-- The action token is the one that can start and stop guests. It
                 stays separate from the read token here because it is separate
                 in the config: an endpoint with only a read token cannot be
                 made to act by accident. -->
            <p class="hint">
              The action token is only used to start, reboot or shut down guests. Without one, those are
              unavailable rather than falling back to the read token.
            </p>

            {#if endpoint.token_set || endpoint.action_token_set}
              <p class="hint">
                Changing the address means sending the token again — homebutler will not carry a saved token
                to a host it was not given for.
              </p>
            {/if}

            <div class="actions">
              <button type="submit" disabled={busy === endpoint.name}>
                {busy === endpoint.name ? 'Saving…' : 'Save'}
              </button>
              {#if confirmRemove === endpoint.name}
                <span class="confirm">
                  Remove {endpoint.name}?
                  <button type="button" class="danger" onclick={() => remove(endpoint.name)}>Remove</button>
                  <button type="button" class="link" onclick={() => (confirmRemove = '')}>Cancel</button>
                </span>
              {:else}
                <button type="button" class="link" onclick={() => (confirmRemove = endpoint.name)}>Remove</button>
              {/if}
            </div>

            {#if message[endpoint.name]}
              <p class:saved={message[endpoint.name].ok} class:error={!message[endpoint.name].ok}>
                {message[endpoint.name].text}
              </p>
            {/if}
          </form>
        {/if}
      </div>
    {/each}

    {#if added}
      <div class="endpoint new">
        <div class="endpoint-header"><span class="endpoint-name">New endpoint</span></div>
        <form class="fields" onsubmit={add}>
          <label class="field">
            <span class="label">Name</span>
            <input name="new.name" autocomplete="off" required bind:value={added.name} />
          </label>
          <div class="pair">
            <label class="field">
              <span class="label">Host</span>
              <input name="new.host" autocomplete="off" spellcheck="false" required bind:value={added.host} />
            </label>
            <label class="field port">
              <span class="label">Port</span>
              <input name="new.port" type="number" min="1" max="65535" placeholder="8006" bind:value={added.port} />
            </label>
          </div>
          <label class="field">
            <span class="label">Token id</span>
            <input name="new.token_id" autocomplete="off" spellcheck="false" required placeholder="root@pam!homebutler" bind:value={added.token_id} />
          </label>
          <label class="field">
            <span class="label">Token</span>
            <input name="new.token" type="password" autocomplete="off" required bind:value={added.token} />
          </label>
          <div class="actions">
            <button type="submit" disabled={busy === 'new'}>{busy === 'new' ? 'Saving…' : 'Add'}</button>
            <button type="button" class="link" onclick={() => (added = null)}>Cancel</button>
          </div>
        </form>
      </div>
    {/if}
  </div>

  {#if !editable}
    <p class="hint read-only">
      Read-only: start <code>homebutler serve</code> with <code>--token</code> to edit endpoints here.
    </p>
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

  .endpoints {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 0.75rem;
  }

  .endpoint {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem 0.85rem;
    background: var(--bg-secondary);
  }

  .endpoint.new {
    border-style: dashed;
  }

  .endpoint-header {
    margin-bottom: 0.6rem;
  }

  .endpoint-name {
    font-weight: 600;
    font-size: 0.9rem;
  }

  .state {
    font-size: 0.7rem;
    color: var(--text-secondary);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
  }

  .state.on {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 50%, transparent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }

  .rows,
  .fields {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    font-size: 0.8rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .pair {
    display: flex;
    gap: 0.5rem;
  }

  .pair .field {
    flex: 1;
  }

  .pair .port {
    max-width: 6.5rem;
  }

  .label {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .control {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  input {
    min-width: 0;
    width: 100%;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.4rem 0.55rem;
    color: var(--text-primary);
    font-family: inherit;
    font-size: 0.85rem;
  }

  input:disabled {
    opacity: 0.5;
    text-decoration: line-through;
  }

  .value {
    color: var(--text-primary);
    overflow-wrap: anywhere;
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

  .hint {
    font-size: 0.72rem;
    line-height: 1.5;
    color: var(--text-secondary);
    margin: 0;
  }

  .hint.read-only {
    margin-top: 0.75rem;
    font-size: 0.78rem;
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

  .error {
    font-size: 0.78rem;
    color: var(--red, #e5534b);
    margin: 0;
  }

  .section-notice {
    margin-bottom: 0.6rem;
    line-height: 1.5;
  }

  @media (max-width: 480px) {
    .endpoints {
      grid-template-columns: 1fr;
    }

    .section-header {
      flex-wrap: wrap;
    }
  }
</style>
