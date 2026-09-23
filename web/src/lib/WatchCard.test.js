import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import WatchCard from './WatchCard.svelte';
import { getWatch, getWatchIncidents, getWatchIncident } from './api.js';

vi.mock('./api.js', () => ({
  getWatch: vi.fn(),
  getWatchIncidents: vi.fn(),
  getWatchIncident: vi.fn(),
  addWatchTarget: vi.fn(),
  removeWatchTarget: vi.fn(),
  checkWatchTargets: vi.fn(),
}));

function overview(extra = {}) {
  return {
    targets: [{ container: 'plex', kind: 'docker', unit: 'plex', added_at: new Date().toISOString() }],
    service: { installed: false },
    retention: { kept: 1, max: 50 },
    ...extra,
  };
}

async function show(data, incidents = [], props = {}) {
  getWatch.mockResolvedValue(data);
  getWatchIncidents.mockResolvedValue(incidents);
  render(WatchCard, props);
  await vi.waitFor(() => expect(getWatch).toHaveBeenCalled());
  await new Promise(r => setTimeout(r, 0));
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(cleanup);

describe('the watch service', () => {
  // The state doctor warns about is the one worth showing first: targets on
  // the list with nothing installed means no incident is ever recorded.
  it('says when nothing is installed to poll the list', async () => {
    await show(overview());
    expect(screen.getByText(/no service installed to check them/)).toBeTruthy();
    expect(screen.getByText('homebutler watch install')).toBeTruthy();
  });

  it('names the unit when one is installed', async () => {
    await show(overview({ service: { installed: true, unit: '/etc/systemd/dev.homebutler.watch.service' } }));
    expect(screen.getByText(/watched by an installed service/)).toBeTruthy();
    expect(screen.queryByText('homebutler watch install')).toBeNull();
  });

  it('says nothing about a service when nothing is watched', async () => {
    await show(overview({ targets: [], retention: { kept: 0, max: 50 } }));
    expect(screen.queryByText(/no service installed/)).toBeNull();
    expect(screen.getByText(/Nothing is being watched/)).toBeTruthy();
  });
});

describe('incidents', () => {
  it('shows why a container went down, not just that it did', async () => {
    await show(overview(), [
      {
        id: 'plex-1',
        container: 'plex',
        detected_at: new Date().toISOString(),
        restart_count: 4,
        exit_code: 137,
        oom_killed: true,
        flapping: { IsFlapping: true, Count: 4, Window: '10m' },
      },
    ]);

    expect(screen.getByText('OOM killed')).toBeTruthy();
    expect(screen.getByText('exit 137')).toBeTruthy();
    expect(screen.getByText(/flapping · 4 in 10m/)).toBeTruthy();
  });

  // exit 0 is a real exit code and has to survive the falsy check that would
  // treat it as "not reported" (#108 draws the same line on the Go side).
  it('shows a zero exit code rather than hiding it', async () => {
    await show(overview(), [
      { id: 'gitea-1', container: 'gitea', detected_at: new Date().toISOString(), restart_count: 1, exit_code: 0 },
    ]);
    expect(screen.getByText('exit 0')).toBeTruthy();
  });

  it('says so when nothing has restarted', async () => {
    await show(overview(), []);
    expect(screen.getByText(/No incidents recorded/)).toBeTruthy();
  });
});

describe('what the card offers to do', () => {
  // Without a token the action routes are not registered, so every one of
  // these would answer 404 — which reads the same as a feature that was never
  // built. The default is off for the same reason: a card mounted without
  // being told shows nothing it cannot do.
  it('offers nothing when the dashboard cannot act', async () => {
    await show(overview());
    expect(screen.getByText('plex')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Check now' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Stop watching/ })).toBeNull();
    expect(screen.queryByLabelText('Container to watch')).toBeNull();
  });

  it('offers all three when it can act', async () => {
    await show(overview(), [], { canAct: true });
    expect(screen.getByRole('button', { name: 'Check now' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Stop watching plex' })).toBeTruthy();
    expect(screen.getByLabelText('Container to watch')).toBeTruthy();
  });
});
