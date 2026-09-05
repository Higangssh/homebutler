import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ProxmoxCard from './ProxmoxCard.svelte';
import { getProxmoxEndpoints, getProxmoxStatus } from './api.js';

vi.mock('./api.js', () => ({
  getProxmoxEndpoints: vi.fn(),
  getProxmoxStatus: vi.fn(),
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
