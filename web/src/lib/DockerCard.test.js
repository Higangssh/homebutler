import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DockerCard from './DockerCard.svelte';
import { getDocker } from './api.js';

vi.mock('./api.js', () => ({
  getDocker: vi.fn(),
  restartContainer: vi.fn(),
  stopContainer: vi.fn(),
}));

const containers = [
  { id: 'a1', name: 'nginx', image: 'nginx:1.25', status: 'Up 4 days', state: 'running' },
  { id: 'f6', name: 'backup', image: 'restic/restic', status: 'Stopped', state: 'exited' },
];

async function show(props = {}) {
  getDocker.mockResolvedValue({ available: true, containers });
  render(DockerCard, props);
  await vi.waitFor(() => expect(getDocker).toHaveBeenCalled());
  await new Promise(r => setTimeout(r, 0));
}

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

describe('what the card offers to do', () => {
  // Without a token the action routes are not registered at all, so a button
  // would answer 404 — indistinguishable from a version that never had the
  // feature. A button that cannot work is worse than no button.
  it('offers nothing when the dashboard cannot act', async () => {
    await show({ canAct: false });
    expect(screen.getByText('nginx')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Restart/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Stop/ })).toBeNull();
  });

  it('offers both actions on a running container when it can act', async () => {
    await show({ canAct: true });
    expect(screen.getByRole('button', { name: 'Restart nginx' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Stop nginx' })).toBeTruthy();
  });

  // Neither action means anything against a container that is already down.
  it('offers nothing on a container that is not running', async () => {
    await show({ canAct: true });
    expect(screen.queryByRole('button', { name: /backup/ })).toBeNull();
  });
});
