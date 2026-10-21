import { defineConfig, devices } from '@playwright/test'

// Browser tests against a running Mediarium with a fresh, empty config
// folder (the first test goes through the setup wizard). Point them at it
// with MEDIARIUM_URL; see e2e/README.md.
const channel = process.env.PW_CHANNEL || undefined

// The same tests run at three sizes. `--project app` is the desktop run;
// `--project app-tablet` and `--project app-phone` reuse the administrator
// the setup project created.
const sizes = {
  tablet: { viewport: { width: 820, height: 1180 }, deviceScaleFactor: 1, hasTouch: true },
  phone: { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
}

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.MEDIARIUM_URL ?? 'http://localhost:8264',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    // Locally an installed Edge or Chrome saves downloading a browser.
    channel,
  },
  projects: [
    { name: 'setup', testMatch: /setup\.spec\.ts/, use: { ...devices['Desktop Chrome'], channel } },
    {
      name: 'app',
      testIgnore: /setup\.spec\.ts/,
      dependencies: ['setup'],
      use: { ...devices['Desktop Chrome'], channel, storageState: 'e2e/.auth/admin.json' },
    },
    ...Object.entries(sizes).map(([name, size]) => ({
      name: `app-${name}`,
      testIgnore: /setup\.spec\.ts/,
      dependencies: ['setup'],
      use: { ...devices['Desktop Chrome'], channel, ...size, storageState: 'e2e/.auth/admin.json' },
    })),
  ],
})
