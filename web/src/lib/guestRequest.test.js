import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { setToken, clearToken, startGuest, shutdownGuest } from './api.js';

// What actually goes on the wire. ProxmoxCard.test.js mocks these functions to
// check which one a button calls; this checks that the function builds the
// request the server requires — a guest is addressed explicitly, and a body
// without node and type is refused.
describe('a guest action request', () => {
  const guest = { vmid: 100, name: 'docker-host', type: 'qemu', node: 'pve1', status: 'running' };

  beforeEach(() => {
    setToken('t');
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200 })));
  });

  afterEach(() => {
    clearToken();
    vi.unstubAllGlobals();
  });

  it('puts the vmid in the path and the node and type in the body', async () => {
    await startGuest(guest, 'pve');
    const [url, opts] = fetch.mock.calls[0];
    expect(url).toBe('/api/proxmox/guests/100/start?endpoint=pve');
    expect(JSON.parse(opts.body)).toEqual({ node: 'pve1', type: 'qemu' });
  });

  // The tier is in the request, not only in the dialog. A shutdown that sent
  // no confirm would be refused by the server, and a shutdown that carried one
  // without asking would be the dialog doing nothing.
  it('adds the confirmation only for shutdown', async () => {
    await shutdownGuest(guest, 'pve');
    const [, opts] = fetch.mock.calls[0];
    expect(JSON.parse(opts.body)).toEqual({ node: 'pve1', type: 'qemu', confirm: true });
  });

  it('leaves the endpoint out when there is none to name', async () => {
    await startGuest(guest, '');
    expect(fetch.mock.calls[0][0]).toBe('/api/proxmox/guests/100/start');
  });
});
