import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AppsCard from './AppsCard.svelte';
import { getInstallable, getInstallStatus, purgeApp, uninstallApp } from './api.js';

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
  { name: 'portainer', description: 'Docker management GUI', default_port: '9443', container_port: '9443' },
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

  // The card said "Removed, with its data." after every purge, including one
  // that left a volume another container was using. `install purge` says
  // "Removed, except:" and lists it; the card has to say the same thing.
  describe('after a purge', () => {
    async function purgeWith(answer) {
      purgeApp.mockResolvedValue(answer);
      await show({ canAct: true });
      await screen.getByRole('button', { expanded: false }).click();
      await vi.waitFor(() => screen.getByRole('button', { name: /Remove portainer and its data/ }));
      await screen.getByRole('button', { name: /Remove portainer and its data/ }).click();

      const dialog = within(screen.getByRole('alertdialog'));
      await fireEvent.input(dialog.getByRole('textbox'), { target: { value: 'portainer' } });
      await dialog.getByRole('button', { name: 'Remove portainer and its data' }).click();
      await vi.waitFor(() => expect(purgeApp).toHaveBeenCalledWith('portainer'));
      await new Promise(r => setTimeout(r, 0));
    }

    it('says it removed everything when nothing was left', async () => {
      await purgeWith({ status: 'purged', app: 'portainer', data_preserved: false });
      await vi.waitFor(() => expect(screen.getByText('Removed, with its data.')).toBeTruthy());
      expect(screen.queryByText(/Removed, except/)).toBeNull();
    });

    it('names the volumes it kept or could not check', async () => {
      await purgeWith({
        status: 'purged',
        app: 'portainer',
        data_preserved: false,
        kept_volumes: ['shared-vol'],
        unchecked_volumes: ['unknown-vol'],
      });
      await vi.waitFor(() => expect(screen.getByText(/Removed, except/)).toBeTruthy());
      expect(screen.queryByText('Removed, with its data.')).toBeNull();
      expect(screen.getByText(/shared-vol/).textContent).toContain('(still exists)');
      expect(screen.getByText(/unknown-vol/).textContent).toContain('(could not check whether it was removed)');
    });
  });

  // An earlier filebrowser install could leave its database in an anonymous
  // volume with no container left to copy it from (#327). The route says so
  // in `warning`, and the card has to say it beside "still on disk".
  describe('after an uninstall', () => {
    async function uninstallWith(answer) {
      uninstallApp.mockResolvedValue(answer);
      await show({ canAct: true });
      await screen.getByRole('button', { expanded: false }).click();
      await vi.waitFor(() => screen.getByRole('button', { name: 'Uninstall' }));
      await screen.getByRole('button', { name: 'Uninstall' }).click();
      await vi.waitFor(() => expect(uninstallApp).toHaveBeenCalledWith('portainer'));
      await new Promise(r => setTimeout(r, 0));
    }

    it('says only that the data is on disk when nothing was stranded', async () => {
      await uninstallWith({ status: 'uninstalled', app: 'portainer', data_preserved: true });
      await vi.waitFor(() => expect(screen.getByText(/Its data is still on disk/)).toBeTruthy());
      expect(screen.queryByText(/anonymous Docker volumes/)).toBeNull();
    });

    it('shows the warning when part of the data is not in the app directory', async () => {
      await uninstallWith({
        status: 'uninstalled',
        app: 'portainer',
        data_preserved: true,
        warning: "portainer's /database are not in /home/u/.homebutler/apps/portainer: an earlier install kept them in anonymous Docker volumes",
      });
      await vi.waitFor(() => expect(screen.getByText(/Its data is still on disk/)).toBeTruthy());
      expect(screen.getByText(/anonymous Docker volumes/).className).toContain('partial');
    });
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
