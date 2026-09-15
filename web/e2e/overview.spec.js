import { expect, test } from './fixtures.js';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

// One request for every machine. The card this replaced asked for the server
// list and then fetched each server in sequence.
test('reads every server in one request', async ({ page }) => {
  const overview = page.waitForResponse(r => r.url().includes('/api/overview'));
  await page.reload();
  await overview;

  const perServer = [];
  page.on('request', r => {
    if (/\/api\/servers\/.+\/status/.test(r.url())) perServer.push(r.url());
  });

  await expect(page.locator('.server-item')).toHaveCount(6);
  expect(perServer).toEqual([]);
});

test('says how old a stale reading is instead of showing it as current', async ({ page }) => {
  const stale = page.locator('.server-item').filter({ hasText: 'media-server' });

  // The numbers are still worth showing — the machine was fine six minutes ago.
  await expect(stale).toContainText('CPU');
  await expect(stale).toContainText('last answered');
  await expect(stale).toContainText('unreachable');
});

test('a server with nothing cached shows the class, not a blank card', async ({ page }) => {
  const never = page.locator('.server-item').filter({ hasText: 'backup-nas' });
  await expect(never).toContainText('authentication');
  await expect(never).not.toContainText('CPU');
});

// The one class an operator has to act on from a terminal rather than the page.
test('a host key failure says so in its own words', async ({ page }) => {
  const hostKey = page.locator('.server-item').filter({ hasText: 'vpn-gateway' });
  await expect(hostKey).toContainText('host_key');
  await expect(hostKey).toContainText('does not match the one homebutler trusts');

  // Since #194 a server that signs in with a password stops here the first
  // time on purpose, and a server added from the settings screen is exactly
  // the case that hits it. The card says where to go — the fingerprint has to
  // be checked somewhere other than this page for checking it to mean
  // anything, so there is no button here that would trust whatever answered.
  await expect(hostKey).toContainText('homebutler trust vpn-gateway');
  await expect(hostKey.getByRole('button', { name: /trust/i })).toHaveCount(0);
});
