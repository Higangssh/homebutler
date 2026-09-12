import { test as base } from '@playwright/test';

export const TOKEN = 'e2e-token';

// Most specs are not about authentication, so they start already holding the
// token — stored exactly where the dashboard stores it, rather than through a
// side door the product does not have.
export const test = base.extend({
  page: async ({ page }, use) => {
    await page.addInitScript(token => {
      window.localStorage.setItem('homebutler.token', token);
    }, TOKEN);
    await use(page);
  },
});

export { expect } from '@playwright/test';
