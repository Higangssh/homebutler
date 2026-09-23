import { expect, test } from './fixtures.js';

const app = (page, name) =>
  page.locator('.app').filter({ has: page.locator('.name', { hasText: new RegExp(`^${name}$`) }) });

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Apps' }).click();
  await page.getByRole('heading', { name: 'Apps' }).waitFor();
});

// Almost every entry in the catalogue has never been installed. That is a
// state, not a failure — the route answered 500 for it until this screen asked.
test('an app that has never been installed reads as a state', async ({ page }) => {
  const row = app(page, 'portainer');
  await row.getByRole('button').first().click();
  await expect(row.getByText('Not installed')).toBeVisible();
  await expect(row.getByRole('button', { name: 'Install' })).toBeVisible();
});

// install_app answers 200 with status "failed" and the reasons when the
// pre-flight refuses. Treating that as an error would put the sentence in a
// toast instead of beside the field that fixes it.
test('a refused pre-flight is an answer, next to the port that caused it', async ({ page }) => {
  await page.route('**/api/install/portainer', async route => {
    if (route.request().method() !== 'POST') return route.continue();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'failed',
        app: 'portainer',
        port: '9443',
        issues: ['port 9443 is already in use by nginx'],
      }),
    });
  });

  const row = app(page, 'portainer');
  await row.getByRole('button').first().click();
  await row.getByRole('button', { name: 'Install' }).click();

  await expect(row.getByText('port 9443 is already in use by nginx')).toBeVisible();
  await expect(row.getByText(/Change the port above/)).toBeVisible();
  // The port field still holds what was tried, so it can be corrected.
  await expect(row.getByLabel('Host port')).toHaveValue('9443');
});

test('installing sends the port that was typed', async ({ page }) => {
  let sent = null;
  await page.route('**/api/install/it-tools', async route => {
    if (route.request().method() !== 'POST') return route.continue();
    sent = route.request().postDataJSON();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'installed', app: 'it-tools', port: '9001', path: '/apps/it-tools' }),
    });
  });

  const row = app(page, 'it-tools');
  await row.getByRole('button').first().click();
  await row.getByLabel('Host port').fill('9001');
  await row.getByRole('button', { name: 'Install' }).click();

  await expect.poll(() => sent).toEqual({ port: '9001' });
});

test.describe('an installed app', () => {
  // uptime-kuma is the one the demo reports as installed, so both states are
  // reachable without the fixture pretending everything is.
  const open = async page => {
    const row = app(page, 'uptime-kuma');
    await row.getByRole('button').first().click();
    await expect(row.getByText(/Installed · running/)).toBeVisible();
    return row;
  };

  test('uninstall runs on the click and says the data is still there', async ({ page }) => {
    const row = await open(page);
    const sent = page.waitForRequest(
      r => r.url().includes('/api/install/uptime-kuma/uninstall') && r.method() === 'POST',
    );
    await row.getByRole('button', { name: 'Uninstall' }).click();
    // Asserted before the request is awaited: a confirmation appearing here is
    // the failure, and it should say so rather than time out waiting for a
    // POST that a dialog is holding back.
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    await sent;
  });

  // The third tier. A click cannot say which thing the operator meant to lose,
  // so the name is the input that carries it — and the button stays dead until
  // the name matches, rather than sending something the server will refuse.
  test('removing the data needs the name typed back', async ({ page }) => {
    const row = await open(page);
    await row.getByRole('button', { name: 'Remove uptime-kuma and its data' }).click();

    const dialog = page.getByRole('alertdialog', { name: 'Permanently remove uptime-kuma' });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('everything it has stored will be deleted');
    await expect(dialog).toContainText('Nothing here brings it back');

    const confirm = dialog.getByRole('button', { name: /^Remove uptime-kuma and its data$/ });
    await expect(confirm).toBeDisabled();

    await dialog.getByRole('textbox').fill('uptime-kum');
    await expect(confirm).toBeDisabled();

    await dialog.getByRole('textbox').fill('uptime-kuma');
    await expect(confirm).toBeEnabled();

    const sent = page.waitForRequest(
      r => r.url().includes('/api/install/uptime-kuma/purge') && r.method() === 'POST',
    );
    await confirm.click();
    const request = await sent;
    expect(request.postDataJSON()).toEqual({ confirm_name: 'uptime-kuma' });
  });

  test('backing out of the removal sends nothing', async ({ page }) => {
    let called = false;
    await page.route('**/purge', async route => {
      called = true;
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    });

    const row = await open(page);
    await row.getByRole('button', { name: 'Remove uptime-kuma and its data' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Keep it' }).click();

    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    expect(called).toBe(false);
  });
});
