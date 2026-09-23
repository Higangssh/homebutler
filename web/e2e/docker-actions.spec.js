import { expect, test } from './fixtures.js';

const row = (page, name) =>
  page.locator('.container-row').filter({ has: page.locator('.name', { hasText: new RegExp(`^${name}$`) }) });

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.locator('.card').filter({ hasText: 'Docker Containers' }).waitFor();
});

// The first tier: the container comes back, so asking again would be asking
// about something that undoes itself.
test('restart runs on the click that asks for it', async ({ page }) => {
  const sent = page.waitForRequest(
    r => r.url().includes('/api/docker/nginx/restart') && r.method() === 'POST',
  );
  await row(page, 'nginx').getByRole('button', { name: 'Restart' }).click();
  await sent;

  // No dialog appeared on the way.
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
});

// The second tier. What matters is not that something was asked, but that it
// said what would be lost — "are you sure?" is the dialog people learn to
// click through, and the tier that types a name back depends on them not
// having learned that here.
test('stop says what will be down before it does it', async ({ page }) => {
  await row(page, 'nginx').getByRole('button', { name: 'Stop' }).click();

  const dialog = page.getByRole('alertdialog', { name: 'Stop nginx' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('will stop and stay stopped');
  await expect(dialog).toContainText('no way to start it again');

  const sent = page.waitForRequest(
    r => r.url().includes('/api/docker/nginx/stop') && r.method() === 'POST',
  );
  await dialog.getByRole('button', { name: 'Stop it' }).click();
  const request = await sent;

  // The confirmation the server requires is the one the dialog collected.
  expect(request.postDataJSON()).toEqual({ confirm: true });
});

test('backing out of the confirmation sends nothing', async ({ page }) => {
  let called = false;
  await page.route('**/api/docker/*/stop', async route => {
    called = true;
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });

  await row(page, 'redis').getByRole('button', { name: 'Stop' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Keep it running' }).click();

  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  expect(called).toBe(false);
});

// A container that is already down has nothing to restart or stop, and a
// button that fails on click is worse than no button.
test('a stopped container offers neither action', async ({ page }) => {
  const backup = row(page, 'backup');
  await expect(backup).toBeVisible();
  await expect(backup.getByRole('button')).toHaveCount(0);
});
