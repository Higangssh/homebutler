import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AppsCard from './AppsCard.svelte';
import { getInstallable, getInstallStatus } from './api.js';

vi.mock('./api.js', () => ({
  getInstallable: vi.fn(),
  getInstallStatus: vi.fn(),
  installApp: vi.fn(),
  uninstallApp: vi.fn(),
  purgeApp: vi.fn(),
}));

// install.App has no json tags on the Go side, so the catalogue arrives with
// Go field names. The fixture uses them for the same reason the component
// reads them.
const catalogue = [
  { Name: 'portainer', Description: 'Docker management GUI', DefaultPort: '9443', ContainerPort: '9443' },
];

async function show(props = {}, status = { app: 'portainer', installed: true, state: 'running' }) {
  getInstallable.mockResolvedValue(catalogue);
  getInstallStatus.mockResolvedValue(status);
  render(AppsCard, props);
  await vi.waitFor(() => expect(getInstallable).toHaveBeenCalled());
  await new Promise(r => setTimeout(r, 0));
}

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

describe('the app catalogue', () => {
  // Fifteen entries, and almost none of them installed. Asking for every
  // status on every visit would be fifteen requests to answer a question
  // nobody had.
  it('asks for a status only when a row is opened', async () => {
    await show({ canAct: true });
    expect(getInstallStatus).not.toHaveBeenCalled();

    await screen.getByRole('button', { expanded: false }).click();
    await vi.waitFor(() => expect(getInstallStatus).toHaveBeenCalledWith('portainer'));
  });

  it('offers nothing when the dashboard cannot act', async () => {
    await show({ canAct: false });
    await screen.getByRole('button', { expanded: false }).click();
    await vi.waitFor(() => expect(getInstallStatus).toHaveBeenCalled());
    await new Promise(r => setTimeout(r, 0));

    expect(screen.queryByRole('button', { name: 'Uninstall' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Remove portainer and its data/ })).toBeNull();
  });
});
