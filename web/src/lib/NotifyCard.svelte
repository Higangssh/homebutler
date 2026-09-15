<script>
  import { saveNotify, testNotify, StaleConfigError } from './api.js';

  // revision is shared with the thresholds form above: both write the same
  // file, so a save through either one hands back the revision the other must
  // use next. Reporting it upward rather than keeping a copy is what stops the
  // second form from being refused as stale after the first one saved.
  let { channels = [], revision = '', editable = false, onsaved, onreload } = $props();

  // What has been typed, per channel, kept beside the loaded data rather than
  // in it: a refused save leaves the typing on screen instead of reverting it.
  //
  // Seeded up front and again whenever the channels are reloaded, rather than
  // filled in on demand while the form renders — Svelte refuses a state change
  // made during render, and the version that did it lazily left the settings
  // screen stuck on "Loading config…".
  function seed(list) {
    const out = {};
    for (const channel of list) {
      const fields = {};
      // A credential box always starts empty. There is nothing to put in it:
      // the server sends whether one is set, never the value.
      for (const f of channel.fields) fields[f.name] = f.secret ? '' : (f.value ?? '');
      out[channel.channel] = fields;
    }
    return out;
  }

  let drafts = $state(seed(channels));
  $effect(() => {
    drafts = seed(channels);
  });

  let clearing = $state({});
  let confirmRemove = $state('');
  let busy = $state('');
  let message = $state({});
  let stale = $state(false);

  let testing = $state(false);
  let testResults = $state(null);
  let testError = $state('');

  function isCleared(channel, field) {
    return Boolean(clearing[`${channel}.${field}`]);
  }

  function toggleClear(channel, field) {
    const key = `${channel}.${field}`;
    clearing[key] = !clearing[key];
  }

  // A blank credential box means "leave it alone". Taking it for a deletion
  // loses a working setup because somebody edited the server address on the
  // same screen, so clearing is its own action.
  function patchFor(channel) {
    const draft = drafts[channel.channel] ?? {};
    const values = {};
    const secrets = {};

    for (const f of channel.fields) {
      const typed = draft[f.name] ?? '';
      if (f.secret) {
        if (isCleared(channel.channel, f.name)) secrets[f.name] = { clear: true };
        else if (typed !== '') secrets[f.name] = { value: typed };
        continue;
      }
      if (typed !== (f.value ?? '')) values[f.name] = typed;
    }

    const patch = {};
    if (Object.keys(values).length) patch.values = values;
    if (Object.keys(secrets).length) patch.secrets = secrets;
    return patch;
  }

  async function send(channel, patch, done) {
    busy = channel;
    message = {};
    stale = false;
    try {
      const result = await saveNotify(revision, { [channel]: patch });
      onsaved?.(result.revision);
      message = { [channel]: { ok: true, text: done } };
      for (const key of Object.keys(clearing)) {
        if (key.startsWith(`${channel}.`)) clearing[key] = false;
      }
    } catch (err) {
      if (err instanceof StaleConfigError) stale = true;
      message = { [channel]: { ok: false, text: err.message } };
    } finally {
      busy = '';
    }
  }

  async function save(event, channel) {
    event.preventDefault();
    if (busy) return;
    const patch = patchFor(channel);
    if (Object.keys(patch).length === 0) {
      message = { [channel.channel]: { ok: false, text: 'Nothing changed.' } };
      return;
    }
    await send(channel.channel, patch, 'Saved. watch and alerts pick this up when they next start.');
  }

  async function remove(channel) {
    confirmRemove = '';
    await send(channel, { remove: true }, 'Removed.');
  }

  async function runTest() {
    if (testing) return;
    testing = true;
    testError = '';
    testResults = null;
    try {
      const result = await testNotify();
      testResults = result.results ?? [];
    } catch (err) {
      testError = err.message;
    } finally {
      testing = false;
    }
  }

  function label(name) {
    return name.replace(/_/g, ' ').replace(/^./, c => c.toUpperCase());
  }

  const configured = $derived(channels.filter(c => c.configured).length);
</script>

<div class="section">
  <div class="section-header">
    <h2>Notifications</h2>
    <span class="badge">{configured} of {channels.length}</span>
    {#if editable}
      <button type="button" class="test" onclick={runTest} disabled={testing || configured === 0}>
        {testing ? 'Sending…' : 'Send test'}
      </button>
    {/if}
  </div>

  {#if testError}
    <p class="error">{testError}</p>
  {/if}

  {#if testResults}
    <ul class="results">
      {#each testResults as result}
        <li class:failed={!result.sent}>
          <span class="mark">{result.sent ? '✅' : '❌'}</span>
          <span class="result-channel">{result.channel}</span>
          <span class="result-detail">{result.sent ? 'sent' : result.error}</span>
        </li>
      {:else}
        <li><span class="result-detail">No channel was configured to send through.</span></li>
      {/each}
    </ul>
  {/if}

  {#if stale}
    <div class="stale">
      <p>The config file changed on disk since this page loaded it; reload before saving.</p>
      <button type="button" onclick={() => onreload?.()}>Reload</button>
    </div>
  {/if}

  <div class="channels">
    {#each channels as channel (channel.channel)}
      <div class="channel" class:configured={channel.configured}>
        <div class="channel-header">
          <span class="channel-name">{channel.channel}</span>
          <span class="state" class:on={channel.configured}>
            {channel.configured ? 'configured' : 'not set up'}
          </span>
        </div>

        {#if !editable}
          <div class="fields">
            {#each channel.fields as f}
              <div class="row">
                <span class="label">{label(f.name)}</span>
                {#if f.secret}
                  <span class="state" class:on={f.set}>{f.set ? 'set' : 'not set'}</span>
                {:else}
                  <code class="value">{f.value || '—'}</code>
                {/if}
              </div>
            {/each}
          </div>
        {:else}
          <form class="fields" onsubmit={e => save(e, channel)}>
            {#each channel.fields as f}
              <label class="field">
                <span class="label">
                  {label(f.name)}
                  {#if f.optional}<span class="optional">optional</span>{/if}
                  {#if f.secret}
                    <span class="state" class:on={f.set}>{f.set ? 'set' : 'not set'}</span>
                  {/if}
                </span>
                <div class="control">
                  <input
                    type={f.secret ? 'password' : 'text'}
                    name={`${channel.channel}.${f.name}`}
                    autocomplete="off"
                    disabled={isCleared(channel.channel, f.name)}
                    placeholder={f.secret ? (f.set ? 'leave blank to keep' : 'not set') : ''}
                    bind:value={drafts[channel.channel][f.name]}
                  />
                  {#if f.secret && f.set}
                    <button type="button" class="link" onclick={() => toggleClear(channel.channel, f.name)}>
                      {isCleared(channel.channel, f.name) ? 'Keep' : 'Clear'}
                    </button>
                  {/if}
                </div>
              </label>
            {/each}

            <div class="actions">
              <button type="submit" disabled={busy === channel.channel}>
                {busy === channel.channel ? 'Saving…' : 'Save'}
              </button>
              {#if channel.configured}
                {#if confirmRemove === channel.channel}
                  <span class="confirm">
                    Remove {channel.channel}?
                    <button type="button" class="danger" onclick={() => remove(channel.channel)}>Remove</button>
                    <button type="button" class="link" onclick={() => (confirmRemove = '')}>Cancel</button>
                  </span>
                {:else}
                  <button type="button" class="link" onclick={() => (confirmRemove = channel.channel)}>
                    Remove
                  </button>
                {/if}
              {/if}
            </div>
          </form>
        {/if}

        {#if message[channel.channel]}
          <p class:saved={message[channel.channel].ok} class:error={!message[channel.channel].ok}>
            {message[channel.channel].text}
          </p>
        {/if}
      </div>
    {/each}
  </div>

  {#if !editable}
    <p class="hint read-only">
      Read-only: start <code>homebutler serve</code> with <code>--token</code> to edit channels here.
    </p>
  {/if}
</div>

<style>
  /* Styles are scoped per component, so the card this section sits in is
     described here rather than inherited from the screen around it. */
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

  .test {
    margin-left: auto;
  }

  .channels {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 0.75rem;
  }

  .channel {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem 0.85rem;
    background: var(--bg-secondary);
  }

  .channel.configured {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }

  .channel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    margin-bottom: 0.6rem;
  }

  .channel-name {
    font-weight: 600;
    font-size: 0.9rem;
  }

  .state {
    font-size: 0.7rem;
    letter-spacing: 0.02em;
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

  .optional {
    font-size: 0.7rem;
    color: var(--text-secondary);
    font-weight: 400;
  }

  .fields {
    display: flex;
    flex-direction: column;
    gap: 0.55rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
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
    flex: 1;
    min-width: 0;
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

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.15rem;
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

  .results {
    list-style: none;
    margin: 0 0 0.75rem;
    padding: 0.5rem 0.7rem;
    border: 1px solid var(--border);
    border-radius: 6px;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .results li {
    display: flex;
    align-items: baseline;
    gap: 0.45rem;
    font-size: 0.8rem;
  }

  .result-channel {
    font-weight: 600;
  }

  .result-detail {
    color: var(--text-secondary);
    overflow-wrap: anywhere;
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
    margin: 0.5rem 0 0;
  }

  .error {
    font-size: 0.78rem;
    color: var(--red, #e5534b);
    margin: 0.5rem 0 0;
  }

  .hint.read-only {
    margin-top: 0.75rem;
    font-size: 0.78rem;
    color: var(--text-secondary);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    font-size: 0.8rem;
  }

  .value {
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  /* The screen this is used on most is a phone: one channel per row, and the
     inputs keep their full width rather than being squeezed beside a label. */
  @media (max-width: 480px) {
    .channels {
      grid-template-columns: 1fr;
    }

    .section-header {
      flex-wrap: wrap;
    }
  }
</style>
