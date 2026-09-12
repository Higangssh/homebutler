import { defineConfig, devices } from '@playwright/test';

const API_PORT = 8099;
const UI_PORT = 5174;

// The suite runs against a real `homebutler serve --demo`: fixed data, no
// Docker, no SSH, no real system calls, which is what makes a browser assertion
// worth writing at all.
//
// The frontend is served by vite rather than by the Go binary, so a change to a
// component does not need the assets rebuilt and embedded before it can be
// tested. The embedded path is not skipped by that choice — TestFrontendFallback
// and TestSPAFallback_UnknownPath cover it on the Go side.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',

  use: {
    baseURL: `http://127.0.0.1:${UI_PORT}`,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],

  webServer: [
    {
      // Started with a token, so the unauthenticated state is reachable. Every
      // other spec seeds the token the way the dashboard itself stores it.
      command: `go run . serve --demo --token e2e-token --port ${API_PORT}`,
      cwd: '..',
      url: `http://127.0.0.1:${API_PORT}/`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      // --host 127.0.0.1 is not optional: vite binds localhost, which resolves
      // to ::1 first here, and the Go server binds 127.0.0.1. Both have to be
      // on the same address family or the readiness check never connects.
      command: `npm run dev -- --port ${UI_PORT} --strictPort --host 127.0.0.1`,
      url: `http://127.0.0.1:${UI_PORT}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: { HOMEBUTLER_API: `http://127.0.0.1:${API_PORT}` },
    },
  ],
});
