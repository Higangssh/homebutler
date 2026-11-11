import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  clearToken,
  defaultServer,
  getDoctor,
  getReport,
  getStatus,
  getToken,
  onUnauthorized,
  saveSnapshot,
  setToken,
  UnauthorizedError,
} from './api.js';

function jsonResponse(body) {
  return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) };
}

function unauthorized() {
  return { ok: false, status: 401, json: async () => ({}), text: async () => 'unauthorized' };
}

function headersOf(call) {
  return call[1].headers;
}

beforeEach(() => {
  clearToken();
  onUnauthorized(null);
  vi.stubGlobal('fetch', vi.fn());
});

afterEach(() => {
  clearToken();
  onUnauthorized(null);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('token', () => {
  it('sends no Authorization header when no token is set', async () => {
    fetch.mockResolvedValue(jsonResponse({ hostname: 'homelab' }));
    await getStatus();
    expect(headersOf(fetch.mock.calls[0]).Authorization).toBeUndefined();
  });

  it('sends the token as a bearer header once set', async () => {
    fetch.mockResolvedValue(jsonResponse({ hostname: 'homelab' }));
    setToken('secret123');
    await getStatus();
    expect(headersOf(fetch.mock.calls[0]).Authorization).toBe('Bearer secret123');
  });

  it('survives storage that throws, for this page at least', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('private browsing');
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('private browsing');
    });

    setToken('secret123');
    expect(getToken()).toBe('secret123');
  });
});

describe('401', () => {
  it('throws UnauthorizedError rather than a generic failure', async () => {
    fetch.mockResolvedValue(unauthorized());
    await expect(getStatus()).rejects.toBeInstanceOf(UnauthorizedError);
  });

  it('reports to the handler, so one screen answers it instead of each card', async () => {
    const handler = vi.fn();
    onUnauthorized(handler);
    fetch.mockResolvedValue(unauthorized());

    await expect(getStatus()).rejects.toBeInstanceOf(UnauthorizedError);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('leaves other failures as ordinary errors for the card to describe', async () => {
    fetch.mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => 'docker unreachable',
      json: async () => ({}),
    });
    await expect(getStatus()).rejects.toThrow('docker unreachable');
  });
});

describe('a refused token', () => {
  it('is gone from storage once cleared, so a reload does not resend it', () => {
    setToken('wrongtoken');
    expect(getToken()).toBe('wrongtoken');

    clearToken();

    expect(getToken()).toBe('');
    expect(localStorage.getItem('homebutler.token')).toBeNull();
  });
});

// Inside a container the local server is listed but cannot be read, and a
// dashboard that opened on it showed the same refusal in every card.
describe('defaultServer', () => {
  it('opens on the local server when it can be read', () => {
    expect(defaultServer([
      { name: 'pi', local: false, visible: true },
      { name: 'mini', local: true, visible: true },
    ])).toBe('mini');
  });

  it('opens on the first server it can read when the local one is not visible', () => {
    expect(defaultServer([
      { name: 'mini', local: true, visible: false },
      { name: 'pi', local: false, visible: true },
    ])).toBe('pi');
  });

  it('opens on the first server when none is local', () => {
    expect(defaultServer([
      { name: 'pi', local: false, visible: true },
      { name: 'nas', local: false, visible: true },
    ])).toBe('pi');
  });

  it('reads a server list without visible as all visible', () => {
    expect(defaultServer([{ name: 'pi' }, { name: 'mini', local: true }])).toBe('mini');
  });

  it('still names a server when none can be read', () => {
    expect(defaultServer([{ name: 'mini', local: true, visible: false }])).toBe('mini');
    expect(defaultServer([])).toBe('');
  });
});

describe('report requests', () => {
  it('name the selected server', async () => {
    fetch.mockResolvedValue(jsonResponse({}));
    await getReport('pi');
    await getDoctor('pi');
    await saveSnapshot('pi');
    expect(fetch.mock.calls.map(c => c[0])).toEqual([
      '/api/report?server=pi',
      '/api/doctor?server=pi',
      '/api/report/snapshot?server=pi',
    ]);
  });
});
