import { expect, test } from '@playwright/test';

// No fixture here: these are the states a browser arrives in with nothing
// stored, which is every first visit to a dashboard started with --token.
test.describe('a dashboard started with a token', () => {
  test('asks for it once instead of failing every card', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'This dashboard needs a token' })).toBeVisible();
    // The point of the gate: one message, not eleven failures behind it.
    await expect(page.locator('.card')).toHaveCount(1);
    await expect(page.locator('header')).toHaveCount(0);
  });

  test('says so when the token is refused, and does not keep it', async ({ page }) => {
    await page.goto('/');

    await page.getByLabel('Token').fill('not-the-token');
    await page.getByRole('button', { name: 'Unlock' }).click();

    await expect(page.getByText(/was not accepted/)).toBeVisible();
    await expect(page.getByRole('heading', { name: 'This dashboard needs a token' })).toBeVisible();

    // A credential the server refused must not come back on the next load.
    const stored = await page.evaluate(() => window.localStorage.getItem('homebutler.token'));
    expect(stored).toBeNull();
  });

  test('unlocks with the right token and stays unlocked across a reload', async ({ page }) => {
    await page.goto('/');

    await page.getByLabel('Token').fill('e2e-token');
    await page.getByRole('button', { name: 'Unlock' }).click();

    await expect(page.getByRole('button', { name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'This dashboard needs a token' })).toHaveCount(0);

    await page.reload();
    await expect(page.getByRole('button', { name: 'Dashboard' })).toBeVisible();
  });

  test('returns to the gate when the token stops working while the page is open', async ({ page }) => {
    await page.goto('/');
    await page.getByLabel('Token').fill('e2e-token');
    await page.getByRole('button', { name: 'Unlock' }).click();
    await expect(page.getByRole('button', { name: 'Watch' })).toBeVisible();

    // What a server restarted under a different token looks like from here.
    // Nothing has to be clicked: the cards poll, so the next refresh is what
    // notices, which is the path this has to work on.
    await page.route('**/api/**', route => route.fulfill({ status: 401, body: '{"error":"unauthorized"}' }));

    await expect(page.getByRole('heading', { name: 'This dashboard needs a token' })).toBeVisible({
      timeout: 15_000,
    });
  });
});
