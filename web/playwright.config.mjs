import { defineConfig } from '@playwright/test';
import { existsSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const edge = 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe';
const linuxChromium = '/usr/bin/chromium';
const executablePath =
  process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ||
  (process.platform === 'win32' && existsSync(edge)
    ? edge
    : process.platform === 'linux' && existsSync(linuxChromium)
      ? linuxChromium
      : undefined);

const e2eSecretsFile = path.join(os.tmpdir(), `music-e2e-secrets-${process.pid}.json`);
try {
  rmSync(e2eSecretsFile, { force: true });
} catch {
  // ignore
}

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
        MUSIC_SECRETS_FILE: e2eSecretsFile,
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
