import { expect, test } from './fixtures.js';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Report' }).click();
  await page.getByRole('heading', { name: 'What changed' }).waitFor();
});

// The case the whole comparison exists for: a container back under the same
// name as something else leaves every count identical.
test('a container recreated under the same name shows as replaced', async ({ page }) => {
  const replaced = page.locator('.changes li').filter({ hasText: 'vaultwarden' });
  await expect(replaced.locator('.kind')).toHaveText('replaced');
  await expect(replaced).toContainText('4f2a1c → 9b7e03');
});

// The kind is the word the README documents and --json carries, shown as
// itself: what is on the screen is what an agent branches on.
test('the kinds are the documented words', async ({ page }) => {
  const kinds = await page.locator('.changes .kind').allInnerTexts();
  for (const kind of kinds) {
    expect(['gone', 'new', 'replaced', 'image', 'state', 'port', 'disk', 'skipped']).toContain(kind);
  }
});

// An all-clear the report cannot stand behind is worse than no answer, and a
// dashboard is where someone glances and moves on.
test('a comparison that could not be made says so', async ({ page }) => {
  const skipped = page.locator('.changes li').filter({ hasText: 'processes' });
  await expect(skipped.locator('.kind')).toHaveText('skipped');
  await expect(skipped).toContainText('did not answer');
});

// "No changes" means nothing without the window it covers.
test('the header says what the comparison is against', async ({ page }) => {
  await expect(page.getByText(/compared with the snapshot from .* ago/)).toBeVisible();
});

test('what needs attention comes before what merely moved', async ({ page }) => {
  const attention = await page.getByText('NEEDS ATTENTION').boundingBox();
  const changes = await page.getByText('NOTABLE CHANGES').boundingBox();
  expect(attention.y).toBeLessThan(changes.y);
});

// Reading is reading. Saving moves the window every later comparison is
// measured from, so it is a button and a separate request.
test('loading the tab does not save a snapshot', async ({ page }) => {
  let saved = false;
  await page.route('**/api/report/snapshot', route => {
    saved = true;
    route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });

  await page.getByRole('button', { name: 'Dashboard' }).click();
  await page.getByRole('button', { name: 'Report' }).click();
  await page.getByRole('heading', { name: 'What changed' }).waitFor();
  expect(saved).toBe(false);

  await page.getByRole('button', { name: 'Save a snapshot' }).click();
  await expect.poll(() => saved).toBe(true);
});

test.describe('doctor', () => {
  test('findings are ordered by severity', async ({ page }) => {
    const severities = await page.locator('.severity').allInnerTexts();
    const rank = { FAIL: 0, WARN: 1, PASS: 2 };
    const ranks = severities.map(s => rank[s.toUpperCase()]);
    expect(ranks).toEqual([...ranks].sort((a, b) => a - b));
  });

  // A command to copy, not a button. Whether homebutler can carry it out is
  // what runner says; a browser button that ran a shell command would be a
  // different product.
  test('a finding shows the command and who can run it', async ({ page }) => {
    const backup = page.locator('.findings li').filter({ hasText: 'No backup in the last 7 days' });
    await expect(backup.locator('.command')).toHaveText('homebutler backup create');
    await expect(backup).toContainText('an agent can run this (backup_create)');
    await expect(backup.getByRole('button')).toHaveCount(0);
  });

  test('a cli-only finding says where to run it', async ({ page }) => {
    const watch = page.locator('.findings li').filter({ hasText: 'no service runs it' });
    await expect(watch).toContainText('run this where homebutler is installed');
  });
});

// The screen this gets read on is a phone, after a notification.
test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the changes are readable and the tab fits', async ({ page }) => {
    await expect(page.locator('.kind-replaced').first()).toBeVisible();
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(width).toBeLessThanOrEqual(390);
  });
});
