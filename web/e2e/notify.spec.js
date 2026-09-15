import { expect, test } from './fixtures.js';

const ntfy = page => page.locator('.channel').filter({ has: page.locator('.channel-name', { hasText: /^ntfy$/ }) });

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Config' }).click();
  await page.getByRole('heading', { name: 'Notifications' }).waitFor();
});

// The credential half of the form. A box for a credential that is already set
// starts empty, because there is nothing to put in it: the server sends whether
// one is set and never the value. Saving around it must not take the blank for
// a deletion, which is the way a working setup gets lost while someone is
// editing the server address beside it.
test('a credential that is set stays set when the save does not mention it', async ({ page }) => {
  const card = ntfy(page);
  const topic = card.locator('input[name="ntfy.topic"]');

  await expect(topic).toHaveValue('');
  await expect(topic).toHaveAttribute('placeholder', 'leave blank to keep');

  let sent = null;
  await page.route('**/api/config/notify', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ saved: true, revision: 'x', restart_needed: ['watch'] }),
    });
  });

  await card.locator('input[name="ntfy.url"]').fill('https://ntfy.example.com');
  await card.getByRole('button', { name: 'Save' }).click();
  await expect(card.getByText(/^Saved/)).toBeVisible();

  expect(sent.channels.ntfy.values).toEqual({ url: 'https://ntfy.example.com' });
  // Not an empty string, not a placeholder: the key is not in the request.
  expect(sent.channels.ntfy.secrets ?? {}).toEqual({});
});

// Clearing is its own action, and it says what it is about to do first.
test('clearing a credential is deliberate', async ({ page }) => {
  const card = ntfy(page);

  let sent = null;
  await page.route('**/api/config/notify', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
  });

  await card.getByRole('button', { name: 'Clear' }).first().click();
  await card.getByRole('button', { name: 'Save' }).click();
  await expect(card.getByText(/^Saved/)).toBeVisible();

  expect(sent.channels.ntfy.secrets.topic).toEqual({ clear: true });
});

test('removing a channel asks before it does it', async ({ page }) => {
  const card = ntfy(page);

  let sent = null;
  await page.route('**/api/config/notify', async route => {
    sent = route.request().postDataJSON();
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
  });

  await card.getByRole('button', { name: 'Remove' }).click();
  await expect(card.getByText('Remove ntfy?')).toBeVisible();
  expect(sent).toBeNull();

  await card.locator('.confirm').getByRole('button', { name: 'Remove' }).click();
  await expect(card.getByText('Removed.')).toBeVisible();
  expect(sent.channels.ntfy).toEqual({ remove: true });
});

// Setting a channel up and never learning whether anything arrives is the state
// this button exists to end, so it reports every channel, including the one
// that failed and why.
test('the test button reports each channel, failures included', async ({ page }) => {
  await page.getByRole('button', { name: 'Send test' }).click();

  const results = page.locator('.results li');
  await expect(results.filter({ hasText: 'ntfy' })).toContainText('sent');
  await expect(results.filter({ hasText: 'gotify' })).toContainText('401');
});

test('a failed test says so rather than nothing', async ({ page }) => {
  await page.route('**/api/notify/test', route =>
    route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: 'no notification channel is configured yet' }) })
  );

  await page.getByRole('button', { name: 'Send test' }).click();
  await expect(page.getByText('no notification channel is configured yet')).toBeVisible();
});

// No credential value reaches the browser in the first place, so there is
// nothing on the page for a save to send back by accident.
test('no credential value is in the config response', async ({ page }) => {
  const config = await page.evaluate(async () => {
    const res = await fetch('/api/config', {
      headers: { Authorization: 'Bearer ' + window.localStorage.getItem('homebutler.token') },
    });
    return res.json();
  });

  for (const channel of config.notify) {
    for (const field of channel.fields) {
      if (!field.secret) {
        expect(field).not.toHaveProperty('set');
        continue;
      }
      expect(field).not.toHaveProperty('value');
      expect(typeof field.set).toBe('boolean');
    }
  }
});

// The screen this is used on is a phone: someone gets an alert, opens the
// dashboard and fixes the channel that sent it.
test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the settings screen fits the width and the tab can be reached', async ({ page }) => {
    await page.goto('/');

    // The server picker used to sit on top of the tabs, so this tap opened the
    // picker instead of the settings.
    await page.getByRole('button', { name: 'Config' }).click();
    await page.getByRole('heading', { name: 'Notifications' }).waitFor();

    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(width).toBeLessThanOrEqual(390);

    const box = await ntfy(page).locator('input[name="ntfy.url"]').boundingBox();
    expect(box.width).toBeGreaterThan(200);
  });
});
