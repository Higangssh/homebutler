import { expect, test } from './fixtures.js';

test('renders the cards the demo server answers for', async ({ page }) => {
  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'System Status' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Docker Containers' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Server Overview' })).toBeVisible();
});

// A failing endpoint is a branch every card has and nobody reaches on purpose.
test('a failing endpoint is reported by its own card, not by the whole page', async ({ page }) => {
  await page.route('**/api/docker*', route =>
    route.fulfill({ status: 500, body: 'docker daemon unreachable' })
  );

  await page.goto('/');

  const docker = page.locator('.card').filter({ hasText: 'Docker Containers' });
  await expect(docker.locator('.error')).toContainText('docker daemon unreachable');

  // The rest of the dashboard keeps working, which is the reason a 500 is not
  // routed to the same place a 401 is.
  await expect(page.getByRole('heading', { name: 'System Status' })).toBeVisible();
});

test('the config tab shows where the config was read from', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Config' }).click();

  await expect(page.getByText('Config file')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Alert Thresholds' })).toBeVisible();
});
