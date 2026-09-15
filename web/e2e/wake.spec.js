import { expect, test } from './fixtures.js';

const device = (page, name) =>
  page.locator('.device').filter({ has: page.locator('.wake-name', { hasText: new RegExp(`^${name}$`) }) });

// The dashboard's own wake card is a different component from the settings
// form below, and nothing covered it: it can be sending magic packets or be
// replaced by something else entirely and the suite would not notice.
test('the dashboard sends a magic packet from its own card', async ({ page }) => {
  await page.goto('/');

  const card = page.locator('.card').filter({ hasText: 'Wake-on-LAN' });
  await expect(card.getByText('gaming-pc')).toBeVisible();

  const sent = page.waitForRequest(r => r.url().includes('/api/wake/') && r.method() === 'POST');
  await card.getByRole('button', { name: 'Wake' }).first().click();
  await sent;
});

test.describe('the settings form', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Config' }).click();
    await page.getByRole('heading', { name: 'Wake-on-LAN Devices' }).waitFor();
  });

  // Nothing answers a magic packet, so an address with a missing octet is only
  // ever discovered as a machine that did not turn on. The refusal comes from the
  // server, in the sentence config validate uses for the same mistake.
  test('an address that is not one is refused before it is saved', async ({ page }) => {
    await page.getByRole('button', { name: 'Add a device' }).click();
    await page.locator('input[name="new.name"]').fill('nas');
    await page.locator('input[name="new.mac"]').fill('11:22:33:44:55');
    await page.getByRole('button', { name: 'Add', exact: true }).click();

    await expect(page.getByText(/Expected format: AA:BB:CC:DD:EE:FF/)).toBeVisible();
    // What was typed is still on screen to be corrected.
    await expect(page.locator('input[name="new.mac"]')).toHaveValue('11:22:33:44:55');
  });

  test('a device can be added', async ({ page }) => {
    let sent = null;
    await page.route('**/api/config/wake', async route => {
      sent = route.request().postDataJSON();
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
    });

    await page.getByRole('button', { name: 'Add a device' }).click();
    await page.locator('input[name="new.name"]').fill('nas');
    await page.locator('input[name="new.mac"]').fill('11:22:33:44:55:66');
    await page.locator('input[name="new.broadcast"]').fill('192.168.1.255');
    await page.getByRole('button', { name: 'Add', exact: true }).click();

    await expect(page.getByText('Added nas.')).toBeVisible();
    expect(sent.targets).toEqual([{ name: 'nas', mac: '11:22:33:44:55:66', broadcast: '192.168.1.255' }]);
  });

  test('changing an address sends only that device', async ({ page }) => {
    let sent = null;
    await page.route('**/api/config/wake', async route => {
      sent = route.request().postDataJSON();
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
    });

    const card = device(page, 'gaming-pc');
    await card.locator('input[name="gaming-pc.mac"]').fill('AA:BB:CC:DD:EE:00');
    await card.getByRole('button', { name: 'Save' }).click();

    await expect(card.getByText('Saved.')).toBeVisible();
    expect(sent.targets).toHaveLength(1);
    expect(sent.targets[0].name).toBe('gaming-pc');
    expect(sent.targets[0].mac).toBe('AA:BB:CC:DD:EE:00');
  });

  test('removing a device asks before it does it', async ({ page }) => {
    let sent = null;
    await page.route('**/api/config/wake', async route => {
      sent = route.request().postDataJSON();
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, revision: 'x' }) });
    });

    const card = device(page, 'gaming-pc');
    await card.getByRole('button', { name: 'Remove' }).click();
    await expect(card.getByText('Remove gaming-pc?')).toBeVisible();
    expect(sent).toBeNull();

    await card.locator('.confirm').getByRole('button', { name: 'Remove' }).click();

    // The card goes with the device, so the answer belongs to the section.
    await expect(page.getByText('Removed gaming-pc.')).toBeVisible();
    expect(sent.targets).toEqual([{ name: 'gaming-pc', remove: true }]);
  });

  test('a save that changes nothing says so instead of pretending', async ({ page }) => {
    let requested = false;
    await page.route('**/api/config/wake', route => {
      requested = true;
      route.fulfill({ status: 200, contentType: 'application/json', body: '{"saved":true}' });
    });

    const card = device(page, 'gaming-pc');
    await card.getByRole('button', { name: 'Save' }).click();

    await expect(card.getByText('Nothing changed.')).toBeVisible();
    expect(requested).toBe(false);
  });
});
