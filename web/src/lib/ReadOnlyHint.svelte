<script>
  // One wording for every card the dashboard cannot edit without a token.
  // It names each place a token can come from and never says which one this
  // server used: the API does not tell the browser, on purpose.
  let { what } = $props();
</script>

<p class="read-only">
  Read-only: set <code>web.token</code> in the config, or <code>HOMEBUTLER_TOKEN</code> in a
  container, and restart <code>homebutler serve</code> to edit {what} here.
</p>

<style>
  .read-only {
    margin: 0.75rem 0 0;
    font-size: 0.78rem;
    line-height: 1.5;
    color: var(--text-secondary);
  }

  code {
    color: var(--accent);
    font-size: 0.75rem;
  }
</style>
