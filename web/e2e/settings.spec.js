import { expect, test } from './fixtures.js';

// The thresholds form is one of several on this screen now, so its Save button
// is named by the form it belongs to rather than by being the only one.
const saveThresholds = page => page.locator('form.thresholds').getByRole('button', { name: 'Save' });

// Named by the form it belongs to for the same reason: the Config screen has
// number inputs of its own now, and "the first one on the page" is a server's
// port.
const threshold = page => page.locator('form.thresholds input[type="number"]').first();

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Config' }).click();
});

test('thresholds can be edited when the dashboard has a token', async ({ page }) => {
  const cpu = threshold(page);
  await expect(cpu).toHaveValue('90');

  await cpu.fill('75');
  await saveThresholds(page).click();

  // Saving is not the end of it: watch and alerts read the file when they
  // start, so the page says when the change actually takes effect.
  await expect(page.getByText(/watch and alerts pick this up when they next start/)).toBeVisible();
});

// Someone edited the file in an editor while this page had it open. The save
// is refused, and the page says so and offers the one action that helps.
test('a config edited elsewhere is reported, not overwritten', async ({ page }) => {
  await page.route('**/api/config/alerts', route =>
    route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({
        error: 'the config file changed on disk since this page loaded it; reload before saving',
      }),
    })
  );

  const cpu = threshold(page);
  await cpu.fill('65');
  await saveThresholds(page).click();

  await expect(page.getByText(/changed on disk since this page loaded it/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reload' })).toBeVisible();

  // What they typed is still there: reloading is their decision, and doing it
  // for them would throw the edit away.
  await expect(cpu).toHaveValue('65');
});

test('a failed save does not claim to have saved', async ({ page }) => {
  // A value the browser's own bounds accept, refused by the server. Out of
  // range is not the way in: the input carries min and max, so the form never
  // submits and the request is never made.
  await page.route('**/api/config/alerts', route =>
    route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'refusing to save: watch.flapping: Flapping thresholds cannot be negative.' }),
    })
  );

  await threshold(page).fill('65');
  await saveThresholds(page).click();

  await expect(page.getByText(/Flapping thresholds cannot be negative/)).toBeVisible();
  await expect(page.getByText(/^Saved/)).toHaveCount(0);
});

// The bounds on the input are a real guard, not decoration: a threshold above
// 100 never reaches the server because the form will not submit.
test('a threshold outside the range never leaves the page', async ({ page }) => {
  let requested = false;
  await page.route('**/api/config/alerts', route => {
    requested = true;
    route.fulfill({ status: 200, contentType: 'application/json', body: '{"saved":true}' });
  });

  await threshold(page).fill('500');
  await saveThresholds(page).click();
  await page.waitForTimeout(300);

  expect(requested).toBe(false);
});

// The credential half: the page never receives a value for a secret, only
// whether one is set, so there is nothing for it to send back by accident.
test('no credential is ever sent to the browser', async ({ page }) => {
  const response = await page.waitForResponse(r => r.url().includes('/api/config'), { timeout: 5000 }).catch(() => null);
  const body = response ? await response.text() : '';
  if (body) {
    expect(body).not.toContain('••••••');
  }

  const config = await page.evaluate(async () => {
    const res = await fetch('/api/config', {
      headers: { Authorization: 'Bearer ' + window.localStorage.getItem('homebutler.token') },
    });
    return res.json();
  });

  // The notify half of this is in notify.spec.js, field by field.
  for (const server of config.servers) {
    expect(server).not.toHaveProperty('password');
  }
});
