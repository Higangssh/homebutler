<script>
  import { saveServers, StaleConfigError } from './api.js';

  let { servers = [], revision = '', editable = false, onsaved, onreload } = $props();

  // Only what has been typed is kept; the input falls back to what the file
  // holds. A server added here appears in the list on the next render, and a
  // seeded box per server would not have one ready for it.
  let edits = $state({});
  let clearing = $state({});
  let added = $state(null);
  let confirmRemove = $state('');
  let busy = $state('');
  let message = $state({});
  let notice = $state(null);
  let stale = $state(false);

  function shown(server, field, fallback = '') {
    return edits[server.name]?.[field] ?? server[field] ?? fallback;
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

  function isCleared(name) {
    return Boolean(clearing[name]);
  }

  // What the form sends: the fields that differ, and a password only when one
  // was typed or deliberately cleared. An untouched password box means "leave
  // it", which is the difference between editing a server and losing its
  // credential.
  function patchFor(server) {
    const patch = { name: server.name };
    const pending = edits[server.name] ?? {};

    for (const field of ['host', 'user', 'auth']) {
      if (pending[field] !== undefined && pending[field] !== (server[field] ?? '')) {
        patch[field] = pending[field];
      }
    }
    if (pending.port !== undefined && Number(pending.port) !== (server.port ?? 0)) {
      patch.port = Number(pending.port);
    }
    if (pending.rename !== undefined && pending.rename !== server.name && pending.rename !== '') {
      patch.rename = pending.rename;
    }
    if (isCleared(server.name)) patch.password = { clear: true };
    else if (pending.password) patch.password = { value: pending.password };

    return patch;
  }

  function changed(patch) {
    return Object.keys(patch).length > 1;
  }

  async function send(key, servers, done, atSection) {
    busy = key;
    message = {};
    notice = null;
    stale = false;
    try {
      const result = await saveServers(revision, servers);
      onsaved?.(result.revision);
      const text = [done, ...(result.notices ?? [])].join(' ');
      if (atSection) notice = { ok: true, text };
      else message = { [key]: { ok: true, text } };
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

  async function save(event, server) {
    event.preventDefault();
    if (busy) return;
    const patch = patchFor(server);
    if (!changed(patch)) {
      message = { [server.name]: { ok: false, text: 'Nothing changed.' } };
      return;
    }
    if (await send(server.name, [patch], 'Saved.')) {
      delete edits[server.name];
      clearing[server.name] = false;
    }
  }

  async function remove(name) {
    confirmRemove = '';
    await send(name, [{ name, remove: true }], `Removed ${name}.`, true);
  }

  async function add(event) {
    event.preventDefault();
    if (busy) return;
    const server = {
      name: added.name,
      host: added.host,
      auth: added.auth,
    };
    if (added.user) server.user = added.user;
    if (added.port) server.port = Number(added.port);
    if (added.auth === 'password' && added.password) server.password = { value: added.password };

    if (await send('new', [server], `Added ${added.name}.`, true)) added = null;
  }

  function startAdding() {
    added = { name: '', host: '', user: '', port: '', auth: 'key', password: '' };
  }
</script>

<div class="section">
  <div class="section-header">
    <h2>Servers</h2>
    <span class="badge">{servers.length}</span>
    {#if editable && !added}
      <button type="button" class="add" onclick={startAdding}>Add a server</button>
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

  <div class="server-grid">
    {#each servers as server (server.name)}
      <div class="server">
        <div class="server-header">
          <span class="server-name">{server.name}</span>
          {#if server.local}
            <span class="state">local</span>
          {/if}
        </div>

        {#if !editable || server.local}
          <div class="rows">
            <div class="row"><span class="label">Host</span><code class="value">{server.host}</code></div>
            {#if !server.local}
              <div class="row"><span class="label">User</span><code class="value">{server.user || '—'}</code></div>
              <div class="row"><span class="label">Port</span><code class="value">{server.port || 22}</code></div>
              <div class="row"><span class="label">Auth</span><code class="value">{server.auth}</code></div>
              {#if server.auth === 'key' && server.key}
                <div class="row"><span class="label">Key</span><code class="value">{server.key}</code></div>
              {/if}
              {#if server.password_set}
                <div class="row"><span class="label">Password</span><span class="state on">set</span></div>
              {/if}
            {/if}
          </div>
          {#if editable && server.local}
            <p class="hint">This is the machine homebutler runs on; there is nothing to connect to.</p>
          {/if}
        {:else}
          <form class="fields" onsubmit={e => save(e, server)}>
            <label class="field">
              <span class="label">Name</span>
              <input
                name={`${server.name}.rename`}
                autocomplete="off"
                value={shown(server, 'rename', server.name)}
                oninput={e => typed(server.name, 'rename', e.currentTarget.value)}
              />
            </label>
            <label class="field">
              <span class="label">Host</span>
              <input
                name={`${server.name}.host`}
                autocomplete="off"
                spellcheck="false"
                value={shown(server, 'host')}
                oninput={e => typed(server.name, 'host', e.currentTarget.value)}
              />
            </label>
            <div class="pair">
              <label class="field">
                <span class="label">User</span>
                <input
                  name={`${server.name}.user`}
                  autocomplete="off"
                  spellcheck="false"
                  value={shown(server, 'user')}
                  oninput={e => typed(server.name, 'user', e.currentTarget.value)}
                />
              </label>
              <label class="field port">
                <span class="label">Port</span>
                <input
                  name={`${server.name}.port`}
                  type="number"
                  min="1"
                  max="65535"
                  placeholder="22"
                  value={portValue(server)}
                  oninput={e => typed(server.name, 'port', e.currentTarget.value)}
                />
              </label>
            </div>

            <label class="field">
              <span class="label">Signs in with</span>
              <select
                name={`${server.name}.auth`}
                value={shown(server, 'auth', 'key')}
                onchange={e => typed(server.name, 'auth', e.currentTarget.value)}
              >
                <option value="key">an SSH key</option>
                <option value="password">a password</option>
              </select>
            </label>

            {#if shown(server, 'auth', 'key') === 'key'}
              <p class="hint">
                Key: <code>{server.key || 'the default in ~/.ssh'}</code> — changed with
                <code>homebutler init</code>, not from here.
              </p>
            {/if}

            <label class="field">
              <span class="label">
                Password
                <span class="state" class:on={server.password_set}>{server.password_set ? 'set' : 'not set'}</span>
              </span>
              <div class="control">
                <input
                  name={`${server.name}.password`}
                  type="password"
                  autocomplete="off"
                  disabled={isCleared(server.name)}
                  placeholder={server.password_set ? 'leave blank to keep' : 'not set'}
                  value={edits[server.name]?.password ?? ''}
                  oninput={e => typed(server.name, 'password', e.currentTarget.value)}
                />
                {#if server.password_set}
                  <button type="button" class="link" onclick={() => (clearing[server.name] = !isCleared(server.name))}>
                    {isCleared(server.name) ? 'Keep' : 'Clear'}
                  </button>
                {/if}
              </div>
            </label>

            {#if server.password_set}
              <!-- The writer refuses this save rather than sending the stored
                   password to somewhere it was never given for. Saying so
                   before the attempt is kinder than after it. -->
              <p class="hint">
                Changing the address means sending the password again — homebutler will not carry a saved
                password to a machine it was not given for.
              </p>
            {/if}

            <div class="actions">
              <button type="submit" disabled={busy === server.name}>
                {busy === server.name ? 'Saving…' : 'Save'}
              </button>
              {#if confirmRemove === server.name}
                <span class="confirm">
                  Remove {server.name}?
                  <button type="button" class="danger" onclick={() => remove(server.name)}>Remove</button>
                  <button type="button" class="link" onclick={() => (confirmRemove = '')}>Cancel</button>
                </span>
              {:else}
                <button type="button" class="link" onclick={() => (confirmRemove = server.name)}>Remove</button>
              {/if}
            </div>

            {#if message[server.name]}
              <p class:saved={message[server.name].ok} class:error={!message[server.name].ok}>
                {message[server.name].text}
              </p>
            {/if}
          </form>
        {/if}
      </div>
    {/each}

    {#if added}
      <div class="server new">
        <div class="server-header"><span class="server-name">New server</span></div>
        <form class="fields" onsubmit={add}>
          <label class="field">
            <span class="label">Name</span>
            <input name="new.name" autocomplete="off" required bind:value={added.name} />
          </label>
          <label class="field">
            <span class="label">Host</span>
            <input name="new.host" autocomplete="off" spellcheck="false" required bind:value={added.host} />
          </label>
          <div class="pair">
            <label class="field">
              <span class="label">User</span>
              <input name="new.user" autocomplete="off" spellcheck="false" placeholder="root" bind:value={added.user} />
            </label>
            <label class="field port">
              <span class="label">Port</span>
              <input name="new.port" type="number" min="1" max="65535" placeholder="22" bind:value={added.port} />
            </label>
          </div>
          <label class="field">
            <span class="label">Signs in with</span>
            <select name="new.auth" bind:value={added.auth}>
              <option value="key">an SSH key</option>
              <option value="password">a password</option>
            </select>
          </label>
          {#if added.auth === 'password'}
            <label class="field">
              <span class="label">Password</span>
              <input name="new.password" type="password" autocomplete="off" required bind:value={added.password} />
            </label>
          {/if}
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
      Read-only: start <code>homebutler serve</code> with <code>--token</code> to edit servers here.
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

  .server-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 0.75rem;
  }

  .server {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem 0.85rem;
    background: var(--bg-secondary);
  }

  .server.new {
    border-style: dashed;
  }

  .server-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    margin-bottom: 0.6rem;
  }

  .server-name {
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

  input,
  select {
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
    .server-grid {
      grid-template-columns: 1fr;
    }

    .section-header {
      flex-wrap: wrap;
    }
  }
</style>
