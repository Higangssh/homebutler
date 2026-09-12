import { expect, test } from './fixtures.js';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Watch' }).click();
});

test('leads with the state doctor warns about', async ({ page }) => {
  // A watch list with entries and nothing installed to poll it records no
  // incident and sends no notification, which is worth more than a count.
  await expect(page.getByText(/no service installed to check them/)).toBeVisible();
  await expect(page.getByText('homebutler watch install')).toBeVisible();
});

test('lists what is watched, with the backend each target uses', async ({ page }) => {
  const targets = page.locator('.mini-card');
  await expect(targets).toHaveCount(3);
  await expect(targets.filter({ hasText: 'node-exporter' })).toContainText('systemd');
});

test('says why a container went down, not only that it did', async ({ page }) => {
  const plex = page.locator('.incident').filter({ hasText: 'plex' });
  await expect(plex).toContainText('OOM killed');
  await expect(plex).toContainText('exit 137');
  await expect(plex).toContainText('flapping');
});

test('fetches the captured logs only when an incident is opened', async ({ page }) => {
  const plex = page.locator('.incident').filter({ hasText: 'plex' });

  // The list is fetched on every page load, so it must not carry two hundred
  // lines of output per incident.
  await expect(plex).not.toContainText('buffer grew to 2.1 GB');

  const detail = page.waitForResponse(r => /\/api\/watch\/incidents\/.+/.test(r.url()));
  await plex.getByRole('button').click();
  await detail;

  await expect(plex).toContainText('buffer grew to 2.1 GB');
  await expect(plex).toContainText('Before the restart');
});
