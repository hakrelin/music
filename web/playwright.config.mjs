import { defineConfig } from '@playwright/test';
import { existsSync } from 'node:fs';
const edge = 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe';
const executablePath =
  process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ||
  (process.platform === 'win32' && existsSync(edge) ? edge : undefined);
export default defineConfig({
  testDir: './tests',
  timeout: 30000,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:5177',
    viewport: { width: 1440, height: 1000 },
    launchOptions: { executablePath },
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: 'go run ./server/cmd/musicd',
      cwd: '..',
      url: 'http://127.0.0.1:18086/api/healthz',
      reuseExistingServer: !process.env.CI,
      env: {
        MUSIC_HTTP_ADDR: '127.0.0.1:18086',
        MUSIC_PROVIDER: 'demo',
        MUSIC_ROOT_PASSWORD: 'test-room-owner',
        MUSIC_ADMIN_PASSWORD: 'test-room-admin',
      },
    },
    {
      command: 'npm run dev -- --host 127.0.0.1 --port 5177',
      url: 'http://127.0.0.1:5177',
      reuseExistingServer: !process.env.CI,
      env: { MUSIC_DEV_TARGET: 'http://127.0.0.1:18086' },
    },
  ],
});
