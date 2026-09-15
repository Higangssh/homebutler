import { expect, test } from './fixtures.js';

const server = (page, name) =>
  page.locator('.server').filter({ has: page.locator('.server-name', { hasText: new RegExp(`^${name}$`) }) });

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Config' }).click();
  await page.getByRole('heading', { name: 'Servers' }).waitFor();
});

// The rule the writer enforces, arriving where somebody can act on it. A saved
// password is sent to whatever answers at the address in the file, so moving a
// server without re-sending the password would hand it to a machine it was
// never given for.
test('moving a server with a saved password asks for the password again', async ({ page }) => {
  const pi = server(page, 'raspberry-pi');
  await pi.locator('input[name="raspberry-pi.host"]').fill('10.0.0.6');
  await pi.getByRole('button', { name: 'Save' }).click();

  await expect(pi.getByText(/saved password/)).toBeVisible();
  await expect(pi.getByText(/^Saved\./)).toHaveCount(0);
});

test('moving it with the password succeeds', async ({ page }) => {
  let sent = null;
  await page.route('**/api/config/servers', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
  });

  const pi = server(page, 'raspberry-pi');
  await pi.locator('input[name="raspberry-pi.host"]').fill('10.0.0.6');
  await pi.locator('input[name="raspberry-pi.password"]').fill('a-new-one');
  await pi.getByRole('button', { name: 'Save' }).click();

  await expect(pi.getByText(/^Saved\./)).toBeVisible();
  expect(sent.servers[0]).toEqual({ name: 'raspberry-pi', host: '10.0.0.6', password: { value: 'a-new-one' } });
});

// Everything else can be changed without re-sending anything, and an untouched
// password box is not a request to delete the password.
test('a save that does not touch the password does not send one', async ({ page }) => {
  let sent = null;
  await page.route('**/api/config/servers', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
  });

  const pi = server(page, 'raspberry-pi');
  await expect(pi.locator('input[name="raspberry-pi.password"]')).toHaveValue('');
  await pi.locator('input[name="raspberry-pi.user"]').fill('admin');
  await pi.getByRole('button', { name: 'Save' }).click();

  await expect(pi.getByText(/^Saved\./)).toBeVisible();
  expect(sent.servers[0]).not.toHaveProperty('password');
});

// A rename is one operation rather than a delete and an add, so it carries the
// name it should have afterwards and nothing else.
test('renaming sends a rename', async ({ page }) => {
  let sent = null;
  await page.route('**/api/config/servers', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
  });

  const nas = server(page, 'nas-box');
  await nas.locator('input[name="nas-box.rename"]').fill('storage');
  await nas.getByRole('button', { name: 'Save' }).click();

  await expect(nas.getByText(/^Saved\./)).toBeVisible();
  expect(sent.servers[0]).toEqual({ name: 'nas-box', rename: 'storage' });
});

test('removing a server asks before it does it', async ({ page }) => {
  let sent = null;
  await page.route('**/api/config/servers', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
  });

  const nas = server(page, 'nas-box');
  await nas.getByRole('button', { name: 'Remove' }).click();
  await expect(nas.getByText('Remove nas-box?')).toBeVisible();
  expect(sent).toBeNull();

  await nas.locator('.confirm').getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByText('Removed nas-box.')).toBeVisible();
  expect(sent.servers).toEqual([{ name: 'nas-box', remove: true }]);
});

// The key path names a file on the machine homebutler runs on, and picking one
// from a browser is a different thing from typing a password.
test('the key file is shown but not editable', async ({ page }) => {
  const nas = server(page, 'nas-box');
  await expect(nas.getByText(/homebutler init/)).toBeVisible();
  await expect(nas.locator('input[name="nas-box.key"]')).toHaveCount(0);
});

// The machine homebutler is running on has nothing to connect to, so there is
// nothing about it to edit here.
test('the local machine is not an editable server', async ({ page }) => {
  const local = server(page, 'mac-mini');
  await expect(local.getByText('This is the machine homebutler runs on')).toBeVisible();
  await expect(local.locator('input')).toHaveCount(0);
});

test.describe('proxmox', () => {
  const endpoint = page => page.locator('.endpoint').filter({ has: page.locator('.endpoint-name', { hasText: /^pve$/ }) });

  test('the token is reported as set and never sent to the page', async ({ page }) => {
    const pve = endpoint(page);
    await expect(pve.getByText('set').first()).toBeVisible();
    await expect(pve.locator('input[name="pve.token"]')).toHaveValue('');

    const config = await page.evaluate(async () => {
      const res = await fetch('/api/config', {
        headers: { Authorization: 'Bearer ' + window.localStorage.getItem('homebutler.token') },
      });
      return res.json();
    });
    for (const item of config.proxmox) {
      expect(item).not.toHaveProperty('token');
      expect(item).not.toHaveProperty('action_token');
      expect(typeof item.token_set).toBe('boolean');
    }
  });

  test('the action token is separate from the one that reads', async ({ page }) => {
    const pve = endpoint(page);
    await expect(pve.locator('input[name="pve.action_token"]')).toBeVisible();
    await expect(pve.getByText(/start, reboot or shut down guests/)).toBeVisible();
  });
});
