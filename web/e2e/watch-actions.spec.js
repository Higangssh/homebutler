import { expect, test } from './fixtures.js';

const target = (page, name) =>
  page.locator('.mini-card').filter({ has: page.locator('.name', { hasText: new RegExp(`^${name}$`) }) });

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Watch' }).click();
  await page.getByRole('heading', { name: 'Watched' }).waitFor();
});

// All three watch actions are the first tier: a target removed by mistake is a
// target added back, and a check that was not needed costs one poll. None of
// them should stop to ask.
test('checking now runs on the click and says what it found', async ({ page }) => {
  const sent = page.waitForRequest(r => r.url().includes('/api/watch/check') && r.method() === 'POST');
  await page.getByRole('button', { name: 'Check now' }).click();
  await sent;

  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  await expect(page.getByText(/Checked every target just now/)).toBeVisible();
});

test('a target can be added, with the kind it is supervised by', async ({ page }) => {
  let sent = null;
  await page.route('**/api/watch/targets', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ container: 'caddy', kind: 'systemd', added: true }),
    });
  });

  await page.getByLabel('Container to watch').fill('caddy');
  await page.getByLabel('How it is supervised').selectOption('systemd');
  await page.getByRole('button', { name: 'Watch it' }).click();

  await expect.poll(() => sent).toEqual({ container: 'caddy', kind: 'systemd' });
  // The field empties only on a save that took, so a failure leaves what was typed.
  await expect(page.getByLabel('Container to watch')).toHaveValue('');
});

// AddTarget answers added:false for something already on the list. That is not
// an error and it is not a success either, and reporting it as either one
// leaves somebody believing a second copy was made.
test('adding something already watched says so instead of claiming a save', async ({ page }) => {
  await page.route('**/api/watch/targets', route =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ container: 'plex', kind: 'docker', added: false }),
    }),
  );

  await page.getByLabel('Container to watch').fill('plex');
  await page.getByRole('button', { name: 'Watch it' }).click();

  await expect(page.getByText('plex is already on the watch list')).toBeVisible();
  await expect(page.getByLabel('Container to watch')).toHaveValue('plex');
});

test('a target stops being watched on the click, with nothing to confirm', async ({ page }) => {
  const sent = page.waitForRequest(
    r => r.url().includes('/api/watch/targets/plex/remove') && r.method() === 'POST',
  );
  await target(page, 'plex').getByRole('button', { name: 'Stop watching plex' }).click();
  await sent;
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
});
