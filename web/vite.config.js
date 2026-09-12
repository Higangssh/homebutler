import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    conditions: ['module', 'browser', 'production'],
  },
  test: {
    environment: 'jsdom',
    // Component tests only. The end-to-end specs are Playwright's, and loading
    // @playwright/test into a jsdom worker fails in a way that reads like a
    // broken component test.
    include: ['src/**/*.test.js'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // The end-to-end run starts its own `homebutler serve --demo` on a port
      // of its own, so the dev server has to be pointed at it rather than at
      // whatever is on 8080.
      '/api': process.env.HOMEBUTLER_API || 'http://localhost:8080',
    },
  },
});
