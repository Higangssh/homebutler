import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ProxmoxCard from './ProxmoxCard.svelte';
import { getProxmoxEndpoints, getProxmoxStatus, startGuest, rebootGuest, shutdownGuest } from './api.js';

vi.mock('./api.js', () => ({
  getProxmoxEndpoints: vi.fn(),
  getProxmoxStatus: vi.fn(),
  startGuest: vi.fn(),
  rebootGuest: vi.fn(),
  shutdownGuest: vi.fn(),
}));

const updatedAtDate = new Date(Date.now() - 65_000);
updatedAtDate.setMilliseconds(789);
const updatedAt = updatedAtDate.toISOString();
const readable = {
  version: { version: '8.2.0' },
  cluster: [{ type: 'node', name: 'pve1', online: true }],
  resources: {
    nodes: [{ name: 'pve1', status: 'online' }],
    guests: [],
    storage: [],
  },
};

async function show(response) {
  getProxmoxEndpoints.mockResolvedValue([{ name: 'pve' }]);
  getProxmoxStatus.mockResolvedValue(response);
  render(ProxmoxCard);
  return (await screen.findAllByRole('status'))[0];
}

describe('Proxmox freshness states', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(cleanup);

  it('shows a server timestamp for current data', async () => {
    const status = await show({ ...readable, status: 'current', updated_at: updatedAt });
    expect(status.textContent).toContain('Current · Updated 1 minute ago');
    expect(status.querySelector('time').title).toMatch(/ UTC$/);
    expect(status.querySelector('time').title).not.toContain('.789Z');
  });

  it('keeps readable data and names a failed collector', async () => {
    const status = await show({ ...readable, warnings: ['cluster: permission denied'], status: 'partially_readable', updated_at: updatedAt, failed_collectors: ['cluster'], failure_classes: { cluster: 'authorization' }, failure_class: 'authorization' });
    expect(status.textContent).toContain('Partially readable');
    expect(status.textContent).toContain('Failed collectors: cluster: Authorization/ACL failure');
    expect(await screen.findByText('pve1')).not.toBeNull();
  });

  it('labels retained data stale after a failed refresh', async () => {
    const status = await show({ ...readable, warnings: ['cluster: permission denied'], status: 'stale', updated_at: updatedAt, refresh_failed_collectors: ['version', 'cluster', 'resources'], refresh_failure_classes: { version: 'transport', cluster: 'transport', resources: 'transport' }, failure_class: 'transport' });
    expect(status.textContent).toContain('Stale · Last successful update');
    expect(status.textContent).toContain('version: Transport failure');
    expect(await screen.findByText('pve1')).not.toBeNull();
  });

  it('keeps an empty readable collection distinct from failure', async () => {
    await show({ ...readable, status: 'current', updated_at: updatedAt });
    expect(await screen.findByText('No guests found')).not.toBeNull();
    expect(screen.queryByText('Guest inventory unavailable')).toBeNull();
  });

  it('labels ACL-filtered resources instead of calling them empty', async () => {
    await show({ ...readable, resources: {}, status: 'partially_readable', updated_at: updatedAt, failed_collectors: ['resources'], failure_classes: { resources: 'authorization' }, failure_class: 'authorization' });
    expect(await screen.findByText('Node inventory unavailable')).not.toBeNull();
    expect(screen.queryByText('No nodes found')).toBeNull();
  });

  it('shows unavailable without rendering data tables or secrets', async () => {
    const status = await show({ status: 'unavailable', failure_class: 'authentication' });
    expect(status.textContent.replace(/\s+/g, ' ')).toContain('Unavailable · Authentication failed');
    expect(screen.queryByRole('table')).toBeNull();
    expect(document.body.textContent).not.toContain('monitoring@pve!readonly');
  });

  it('labels configuration failures without rendering data', async () => {
    const status = await show({ status: 'unavailable', failure_class: 'configuration' });
    expect(status.textContent.replace(/\s+/g, ' ').trim()).toBe('Unavailable · Configuration failed');
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('labels an unexpected endpoint response', async () => {
    const status = await show({ status: 'unavailable', failure_class: 'response' });
    expect(status.textContent.replace(/\s+/g, ' ')).toContain('Unavailable · Unexpected endpoint response');
  });

  it('does not assign a classified collector reason to an unclassified failure', async () => {
    const status = await show({ status: 'unavailable', failed_collectors: ['version', 'cluster'], failure_classes: { cluster: 'authorization' }, failure_class: 'authorization' });
    expect(status.textContent.replace(/\s+/g, ' ')).toContain('version, cluster: Authorization/ACL failure');
    expect(status.textContent).not.toContain('version: Authorization/ACL failure');
  });

  it('explains an unclassified unavailable response', async () => {
    const status = await show({ status: 'unavailable', message: 'Proxmox status is unavailable' });
    expect(status.textContent.replace(/\s+/g, ' ')).toContain('Unavailable · Proxmox status is unavailable');
  });

  it('shows not configured when there are no endpoints', async () => {
    getProxmoxEndpoints.mockResolvedValue([]);
    render(ProxmoxCard);
    expect((await screen.findByRole('status')).textContent).toContain('Proxmox is not configured');
    expect(getProxmoxStatus).not.toHaveBeenCalled();
  });
});

// The guest actions. Demo mode has no Proxmox endpoint configured, so the
// end-to-end suite cannot reach this screen at all — these are the tests that
// exist for it, and that is worth knowing before trusting a green e2e run to
// have covered the third area of #272.
describe('what the guest table offers to do', () => {
  const guests = [
    { vmid: 100, name: 'docker-host', type: 'qemu', node: 'pve1', status: 'running' },
    { vmid: 200, name: 'adguard', type: 'lxc', node: 'pve1', status: 'stopped' },
    { vmid: 300, name: 'golden', type: 'qemu', node: 'pve1', status: 'stopped', template: true },
  ];

  const withGuests = {
    ...readable,
    status: 'current',
    updated_at: new Date().toISOString(),
    resources: { nodes: [], guests, storage: [] },
  };

  async function showGuests(props = {}) {
    getProxmoxEndpoints.mockResolvedValue([{ name: 'pve' }]);
    getProxmoxStatus.mockResolvedValue(withGuests);
    render(ProxmoxCard, props);
    await screen.findByText('docker-host');
  }

  beforeEach(() => vi.clearAllMocks());
  afterEach(cleanup);

  it('offers nothing when the dashboard cannot act', async () => {
    await showGuests({ canAct: false });
    expect(screen.queryByRole('button', { name: /Start/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Shut down/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Reboot/ })).toBeNull();
  });

  // A stopped guest has nothing to reboot or shut down, and a running one has
  // nothing to start. A button that cannot work is worse than no button.
  it('offers only what the state of the guest allows', async () => {
    await showGuests({ canAct: true });
    expect(screen.getByRole('button', { name: 'Reboot docker-host' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Shut down docker-host' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Start docker-host' })).toBeNull();

    expect(screen.getByRole('button', { name: 'Start adguard' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Shut down adguard' })).toBeNull();
  });

  // A template is not a guest to act on: Proxmox refuses, and offering the
  // button would be offering a failure.
  it('offers nothing on a template', async () => {
    await showGuests({ canAct: true });
    expect(screen.queryByRole('button', { name: /golden/ })).toBeNull();
    expect(screen.getByText('template')).toBeTruthy();
  });
});

// The three tiers, as requests. A guest is addressed explicitly and never
// guessed: the server refuses a body without node and type, because the same
// vmid can exist on two nodes of a cluster.
describe('what the guest actions send', () => {
  const running = { vmid: 100, name: 'docker-host', type: 'qemu', node: 'pve1', status: 'running' };
  const stopped = { vmid: 200, name: 'adguard', type: 'lxc', node: 'pve2', status: 'stopped' };

  async function showGuests() {
    getProxmoxEndpoints.mockResolvedValue([{ name: 'pve' }]);
    getProxmoxStatus.mockResolvedValue({
      ...readable,
      status: 'current',
      updated_at: new Date().toISOString(),
      resources: { nodes: [], guests: [running, stopped], storage: [] },
    });
    render(ProxmoxCard, { canAct: true });
    await screen.findByText('docker-host');
  }

  beforeEach(() => vi.clearAllMocks());
  afterEach(cleanup);

  it('starts on the click, carrying the node and type off the row', async () => {
    startGuest.mockResolvedValue({ status: 'accepted' });
    await showGuests();
    await screen.getByRole('button', { name: 'Start adguard' }).click();
    await vi.waitFor(() => expect(startGuest).toHaveBeenCalledWith(stopped, 'pve'));
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('reboots on the click, with nothing to confirm', async () => {
    rebootGuest.mockResolvedValue({ status: 'accepted' });
    await showGuests();
    await screen.getByRole('button', { name: 'Reboot docker-host' }).click();
    await vi.waitFor(() => expect(rebootGuest).toHaveBeenCalledWith(running, 'pve'));
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  // The second tier. What matters is that it says what will be off and who
  // has to bring it back, not that it asked.
  it('says what shutting down costs before it does it', async () => {
    shutdownGuest.mockResolvedValue({ status: 'accepted' });
    await showGuests();
    await screen.getByRole('button', { name: 'Shut down docker-host' }).click();

    const dialog = screen.getByRole('alertdialog', { name: 'Shut down docker-host' });
    expect(dialog.textContent).toContain('will shut down and stay off');
    expect(dialog.textContent).toContain('until it is started again');
    expect(shutdownGuest).not.toHaveBeenCalled();

    await screen.getByRole('button', { name: 'Shut it down' }).click();
    await vi.waitFor(() => expect(shutdownGuest).toHaveBeenCalledWith(running, 'pve'));
  });

  it('backing out of the shutdown sends nothing', async () => {
    await showGuests();
    await screen.getByRole('button', { name: 'Shut down docker-host' }).click();
    await screen.getByRole('button', { name: 'Leave it running' }).click();
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(shutdownGuest).not.toHaveBeenCalled();
  });
});
