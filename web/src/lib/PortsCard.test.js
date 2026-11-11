import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import PortsCard from './PortsCard.svelte';
import { getPorts } from './api.js';

vi.mock('./api.js', () => ({ getPorts: vi.fn() }));

async function show(answer) {
  getPorts.mockResolvedValue(answer);
  render(PortsCard);
  await vi.waitFor(() => expect(getPorts).toHaveBeenCalled());
  await new Promise(r => setTimeout(r, 0));
}

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

describe('the ports it lists', () => {
  // The card read the answer as a bare list, which /api/ports has not been
  // since 0.11.2, so it showed nothing for this machine — not even its own
  // "No open ports detected".
  it('lists what /api/ports answers with', async () => {
    await show({
      ports: [
        { protocol: 'tcp', address: '0.0.0.0', port: '22', process: 'sshd' },
        { protocol: 'tcp', address: '127.0.0.1', port: '5432' },
      ],
    });
    expect(screen.getByText('2 open')).toBeTruthy();
    expect(screen.getByText(':22')).toBeTruthy();
    expect(screen.getByText('sshd')).toBeTruthy();
    expect(screen.getByText(':5432')).toBeTruthy();
  });

  it('says so when nothing is listening', async () => {
    await show({ ports: [] });
    expect(screen.getByText('No open ports detected')).toBeTruthy();
  });
});
