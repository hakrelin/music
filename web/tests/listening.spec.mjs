import { test, expect } from '@playwright/test';
async function create(request, name = '测试房间', password = '') {
  const response = await request.post('/api/rooms', {
    data: { name: name + ' ' + Date.now(), desc: '浏览器集成测试', needPwd: !!password, password },
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()).id;
}
async function joined(page, id) {
  await page.goto('/room/' + id);
  await expect(page.locator('.room-gate')).toHaveCount(0);
}
async function search(page, word = '恋') {
  await page.getByRole('button', { name: '添加歌曲', exact: true }).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill(word);
  await page.getByRole('button', { name: '搜索音乐', exact: true }).click();
  await expect(page.locator('.track').first()).toBeVisible();
}
async function observeAudio(page) {
  await page.addInitScript(() => {
    const NativeAudio = window.Audio;
    window.Audio = class extends NativeAudio {
      constructor(...args) {
        super(...args);
        window.__testAudio = this;
      }
    };
  });
}
async function ensurePlaying(page) {
  await expect.poll(() => page.evaluate(() => !!window.__testAudio.getAttribute('src'))).toBe(true);
  // Play is idempotent; locating a toggle by its changing label races with autoplay.
  await page.evaluate(async () => {
    const player = document
      .querySelector('#app')
      .__vue_app__.config.globalProperties.$pinia._s.get('player');
    await player.play();
  });
  await expect.poll(() => page.evaluate(() => window.__testAudio.paused)).toBe(false);
}
test('大厅显示真实房间，分类、搜索及创建入口可用', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveTitle('茶与金平糖 · 一起听');
  await expect(page.locator('.room-card').first()).toBeVisible();
  await page.getByRole('button', { name: '点歌房', exact: true }).click();
  await expect(page.locator('.featured')).toBeVisible();
  await page.getByRole('button', { name: '全部房间', exact: true }).click();
  await page.getByRole('textbox', { name: '搜索房间' }).fill('没有这个房间-xyz');
  await expect(page.locator('.room-card')).toHaveCount(0);
  await expect(page.getByText('没有找到这个房间，换个关键词试试。')).toBeVisible();
  await page.getByRole('textbox', { name: '搜索房间' }).clear();
  await page.getByRole('button', { name: '创建房间', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '创建房间', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: '创建房间', exact: true })).toBeFocused();
});
test('密码房创建后直接进入，分享地址不含密码，新访客需要验证', async ({ page, context }) => {
  await page.goto('/');
  await page.getByRole('button', { name: '创建房间', exact: true }).click();
  await page.getByPlaceholder('例如：修图的时候听什么').fill('密码房 ' + Date.now());
  await page.getByLabel('设置房间密码').check();
  await page.getByLabel('密码', { exact: true }).fill('test-secret');
  await page.getByRole('button', { name: '创建并进入', exact: true }).click();
  await page.waitForURL('**/room/**');
  await expect(page.locator('.room-gate')).toHaveCount(0);
  expect(page.url()).not.toContain('test-secret');
  expect(page.url()).not.toContain('pwd=');
  const other = await context.newPage();
  await other.goto(page.url());
  await expect(other.getByRole('heading', { name: '这个房间需要密码' })).toBeVisible();
  await other.getByLabel('房间密码', { exact: true }).fill('wrong');
  await other.getByRole('button', { name: '进入房间', exact: true }).click();
  await expect(other.locator('.room-gate [role=alert]')).toContainText(/密码|口令/);
  await other.getByLabel('房间密码', { exact: true }).fill('test-secret');
  await other.getByRole('button', { name: '进入房间', exact: true }).click();
  await expect(other.locator('.room-gate')).toHaveCount(0);
  await other.close();
});
test('两个独立客户端同步歌曲、聊天和投票切歌', async ({ page, browser, request }) => {
  const id = await create(request, '同步测试');
  await observeAudio(page);
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('竹');
  await page.getByRole('button', { name: '搜索音乐', exact: true }).click();
  await expect(page.locator('.t-name').first()).not.toHaveText('恋之瞳');
  const nextName = await page.locator('.t-name').first().textContent();
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(page.locator('.song-info h1')).toHaveText('恋之瞳');
  const otherContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const other = await otherContext.newPage();
  await observeAudio(other);
  await other.goto(page.url());
  await expect(other.locator('.song-info h1')).toHaveText('恋之瞳');
  for (const client of [page, other]) {
    if (await client.getByRole('button', { name: '开始收听', exact: true }).isVisible())
      await client.getByRole('button', { name: '开始收听', exact: true }).click();
    else if (await client.getByRole('button', { name: '开始同步收听', exact: true }).isVisible())
      await client.getByRole('button', { name: '开始同步收听', exact: true }).click();
    await expect
      .poll(() => client.evaluate(() => window.__testAudio.currentTime))
      .toBeGreaterThan(0);
  }
  const timeA = await page.evaluate(() => window.__testAudio.currentTime);
  const timeB = await other.evaluate(() => window.__testAudio.currentTime);
  expect(Math.abs(timeA - timeB)).toBeLessThan(1.6);
  await page.getByLabel('聊天消息', { exact: true }).fill('双客户端聊天测试');
  await page.getByRole('button', { name: '发送消息', exact: true }).click();
  await expect(other.locator('.bubble').filter({ hasText: '双客户端聊天测试' })).toBeVisible();
  await page.getByTitle('投票切换到下一首', { exact: true }).click();
  await expect(page.locator('.song-info h1')).toHaveText(nextName);
  await expect(other.locator('.song-info h1')).toHaveText(nextName);
  await otherContext.close();
});
test('手机切换面板时持续播放，320/390/768 像素下不横向溢出', async ({ page, request }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await observeAudio(page);
  const id = await create(request, '手机测试');
  await joined(page, id);
  await page.getByRole('button', { name: '点歌', exact: true }).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('恋');
  await page.getByRole('button', { name: '搜索音乐', exact: true }).click();
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await ensurePlaying(page);
  await expect.poll(() => page.evaluate(() => window.__testAudio.paused)).toBe(false);
  await page.getByRole('button', { name: '聊天', exact: true }).click();
  await expect(page.getByLabel('聊天消息', { exact: true })).toBeVisible();
  await expect(page.getByRole('contentinfo', { name: '播放控制' })).toBeVisible();
  expect(await page.evaluate(() => window.__testAudio.paused)).toBe(false);
  await page.getByRole('button', { name: '队列', exact: true }).click();
  await expect(page.locator('.queue-panel')).toBeVisible();
  for (const width of [320, 390, 768]) {
    await page.setViewportSize({ width, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  }
});
test('搜索弹窗锁定焦点并支持 Escape，设置认证及清空待播生效', async ({ page, request }) => {
  const id = await create(request, '管理测试');
  await joined(page, id);
  await search(page);
  for (let i = 0; i < 10; i++) {
    await page.keyboard.press('Tab');
    expect(await page.evaluate(() => !!document.activeElement.closest('dialog[open]'))).toBe(true);
  }
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: '添加歌曲', exact: true })).toBeFocused();
  await page.getByRole('button', { name: '房间设置', exact: true }).click();
  await page.getByPlaceholder('口令', { exact: true }).fill('test-room-owner');
  await page.getByRole('button', { name: '认证', exact: true }).click();
  await expect(page.getByRole('button', { name: '清空待播', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: '清空待播', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(page.locator('.queue-panel .queue-scroll li')).toHaveCount(1);
  await page.getByRole('button', { name: /游客/ }).click();
  await page.getByPlaceholder('输入你的昵称').fill('修嵌测试员');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await expect(page.locator('.room-gate')).toHaveCount(0);
  await page.getByLabel('聊天消息', { exact: true }).fill('昵称更新测试');
  await page.getByRole('button', { name: '发送消息', exact: true }).click();
  await expect(
    page.locator('.message').filter({ hasText: '昵称更新测试' }).locator('.message-meta b'),
  ).toHaveText('修嵌测试员');
});
test('空大厅和接口错误有明确反馈', async ({ page }) => {
  await page.route('**/api/rooms', (route) => route.fulfill({ json: { rooms: [] } }));
  await page.goto('/');
  await expect(page.getByText('还没有房间，来放第一首歌吧。')).toBeVisible();
  await page.unroute('**/api/rooms');
  await page.route('**/api/rooms', (route) =>
    route.fulfill({
      status: 503,
      json: { error: { code: 'unavailable', message: '测试服务暂不可用' } },
    }),
  );
  await page.getByRole('button', { name: '刷新房间列表', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('测试服务暂不可用');
});

test('音量为零能保存，暂停后恢复同步，同曲重播能回到开头', async ({ page, request }) => {
  const id = await create(request, '播放状态测试');
  await observeAudio(page);
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  if (await page.getByRole('button', { name: '开始收听', exact: true }).isVisible())
    await page.getByRole('button', { name: '开始收听', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.__testAudio.currentTime)).toBeGreaterThan(0);
  await page.getByRole('slider', { name: '音量', exact: true }).focus();
  await page.getByRole('slider', { name: '音量', exact: true }).press('Home');
  await expect(page.getByRole('slider', { name: '音量', exact: true })).toHaveValue('0');
  await page.reload();
  await expect(page.locator('.room-gate')).toHaveCount(0);
  await expect(page.getByRole('slider', { name: '音量', exact: true })).toHaveValue('0');
  await ensurePlaying(page);
  await expect.poll(() => page.evaluate(() => window.__testAudio.currentTime)).toBeGreaterThan(2.1);
  await page.getByTitle('投票切换到下一首', { exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.__testAudio.currentTime)).toBeLessThan(1.6);
  await page.getByRole('button', { name: '暂停收听', exact: true }).click();
  expect(await page.evaluate(() => window.__testAudio.paused)).toBe(true);
  await page.getByRole('button', { name: '开始同步收听', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.__testAudio.paused)).toBe(false);
});

test('连续跨过三个同步周期仍持续前进，不回跳到入房时间', async ({ page, request }) => {
  const id = await create(request, '连续播放回归');
  await observeAudio(page);
  await joined(page, id);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', {
    offline: false,
    latency: 120,
    downloadThroughput: 125000,
    uploadThroughput: 125000,
  });
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await ensurePlaying(page);
  await expect.poll(() => page.evaluate(() => window.__testAudio.currentTime)).toBeGreaterThan(0.1);
  let previous = await page.evaluate(() => window.__testAudio.currentTime);
  for (let cycle = 0; cycle < 3; cycle++) {
    // This real-time wait crosses the 5-second correction timer while native audio keeps playing.
    await page.waitForTimeout(5200);
    const sample = await page.evaluate(() => {
      const a = window.__testAudio;
      const p = document
        .querySelector('#app')
        .__vue_app__.config.globalProperties.$pinia._s.get('player');
      return {
        time: a.currentTime,
        target: (Date.now() + p.offset - p.startedAt) / 1000,
        paused: a.paused,
      };
    });
    expect(sample.paused).toBe(false);
    expect(sample.time - previous).toBeGreaterThan(4.1);
    expect(Math.abs(sample.time - sample.target)).toBeLessThan(1.6);
    previous = sample.time;
  }
});

test('音源失败会重新加载，网络错误不会冒充自动播放被阻止', async ({ page, request }) => {
  const id = await create(request, '音源重试回归');
  await observeAudio(page);
  let failures = 0;
  await page.route('**/api/demo/audio/**', async (route) => {
    if (failures++ === 0) await route.fulfill({ status: 503, body: 'temporarily unavailable' });
    else await route.continue();
  });
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  // Browser gesture unlocks audio; a failed response must then be reloaded automatically.
  await ensurePlaying(page);
  await expect
    .poll(() => page.evaluate(() => window.__testAudio.currentTime), { timeout: 12000 })
    .toBeGreaterThan(0.1);
  expect(failures).toBeGreaterThan(1);
  await expect(page.getByText('浏览器需要你点击一次，才能开始播放。')).toHaveCount(0);
  await expect(page.locator('.play-notice.failed')).toHaveCount(0);
});

test('网易云节点 403 时改连等价 CDN，保留签名路径且恢复播放', async ({ page, request }) => {
  const id = await create(request, 'CDN 重试回归');
  const sample = await request.get('/api/demo/audio/d19.wav');
  expect(sample.ok()).toBeTruthy();
  const audioBody = await sample.body();
  const hosts = [];
  await page.route('https://m704.music.126.net/**', async (route) => {
    hosts.push('m704');
    await route.fulfill({ status: 403, contentType: 'text/html', body: 'Forbidden' });
  });
  await page.route('https://m701.music.126.net/**', async (route) => {
    hosts.push('m701');
    await route.fulfill({ status: 200, contentType: 'audio/wav', body: audioBody });
  });
  await observeAudio(page);
  await joined(page, id);
  await page.getByRole('button', { name: '房间设置', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.evaluate(() => {
    const pinia = document.querySelector('#app').__vue_app__.config.globalProperties.$pinia;
    const p = pinia._s.get('player');
    p.load(
      {
        id: 'cdn-test',
        source: 'wy',
        name: 'CDN test',
        artist: '',
        album: '',
        cover: '',
        duration: 32,
        fee: 0,
        url: 'https://m704.music.126.net/20990101/signature/jdymusic/obj/test.mp3?token=preserved',
      },
      Date.now(),
      0,
      true,
    );
  });
  await expect
    .poll(() => page.evaluate(() => window.__testAudio.currentTime), { timeout: 10000 })
    .toBeGreaterThan(0.1);
  expect(hosts).toEqual(['m704', 'm701']);
  expect(await page.evaluate(() => window.__testAudio.getAttribute('src'))).toBe(
    'https://m701.music.126.net/20990101/signature/jdymusic/obj/test.mp3?token=preserved',
  );
});
