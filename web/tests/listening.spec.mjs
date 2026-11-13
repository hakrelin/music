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
    Object.defineProperty(window, '__testAudio', {get() {
      return document.querySelector('#app')?.__vue_app__?.config.globalProperties.$pinia._s.get('player')?.media || window.__firstAudio;
    }});
    const NativeAudio = window.Audio;
    window.Audio = class extends NativeAudio {
      constructor(...args) {
        super(...args);
        window.__firstAudio ||= this;
        (window.__testAudioInstances ||= []).push(this);
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

test('收起封面与侧栏释放歌词空间，刷新后保留选择且不打断播放', async ({ page, request }) => {
  const id = await create(request, '布局偏好');
  await observeAudio(page);
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await ensurePlaying(page);
  const before = await page.locator('.lyrics').boundingBox();
  await page.evaluate(() => { window.__layoutAudio = window.__testAudio; });
  await page.getByRole('button', { name: '隐藏封面', exact: true }).click();
  await expect(page.locator('.album-stage')).toHaveCount(0);
  await expect(page.locator('.bar-cover')).toBeVisible();
  await expect(page.locator('.queue-cover img').first()).toBeVisible();
  const after = await page.locator('.lyrics').boundingBox();
  expect(after.height - before.height).toBeGreaterThan(100);
  await page.getByRole('button', { name: '队列', exact: true }).click();
  await page.getByRole('button', { name: '聊天', exact: true }).click();
  await expect(page.locator('.queue-panel')).toBeHidden();
  await expect(page.locator('.chat-panel')).toBeHidden();
  expect((await page.locator('.lyrics').boundingBox()).width - before.width).toBeGreaterThan(400);
  expect(await page.evaluate(() => window.__layoutAudio === window.__testAudio && !window.__testAudio.paused)).toBe(true);
  // 点歌仍有独立入口；收起队列后也能打开。
  await page.getByRole('button', { name: '点歌', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '点一首歌', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.reload();
  await expect(page.locator('.room-gate')).toHaveCount(0);
  await expect(page.getByRole('button', { name: '显示封面', exact: true })).toBeVisible();
  await expect(page.locator('.album-stage')).toHaveCount(0);
  await expect(page.locator('.bar-cover')).toBeVisible();
  await expect(page.locator('.queue-panel')).toBeHidden();
  await expect(page.locator('.chat-panel')).toBeHidden();
  // 桌面收起偏好不影响手机通过标签访问队列和聊天。
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: '队列', exact: true }).click();
  await expect(page.locator('.queue-panel')).toBeVisible();
  await expect(page.locator('.queue-cover img').first()).toBeVisible();
  await page.getByRole('button', { name: '聊天', exact: true }).click();
  await expect(page.getByLabel('聊天消息', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '正在听', exact: true }).click();
  await expect(page.locator('.bar-cover')).toBeVisible();
  await expect(page.locator('.album-stage')).toHaveCount(0);
  await page.getByRole('button', { name: '显示封面', exact: true }).click();
  await expect(page.locator('.album-stage')).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole('button', { name: '队列', exact: true }).click();
  await page.getByRole('button', { name: '聊天', exact: true }).click();
  await expect(page.locator('.queue-panel')).toBeVisible();
  await expect(page.locator('.chat-panel')).toBeVisible();
});

test('播放页适应矮屏与手机，歌词和聊天输入不被播放条遮挡', async ({ page, request }) => {
  const id = await create(request, '视口布局');
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  for (const [width, height] of [[320, 568], [390, 844], [768, 1024], [1024, 768], [1440, 900], [844, 390]]) {
    await page.setViewportSize({ width, height });
    if (width <= 900) await page.getByRole('button', { name: '正在听', exact: true }).click();
    for (const cover of [true, false]) {
      await expect(page.getByRole('button', { name: cover ? '隐藏封面' : '显示封面', exact: true })).toBeVisible();
      const lyrics = await page.locator('.lyrics').boundingBox();
      const bar = await page.locator('.player-bar').boundingBox();
      expect(lyrics.height).toBeGreaterThan(40);
      expect(lyrics.y + lyrics.height).toBeLessThanOrEqual(bar.y);
      expect(bar.y + bar.height).toBeLessThanOrEqual(height);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth && document.documentElement.scrollHeight <= innerHeight)).toBe(true);
      await page.getByRole('button', { name: cover ? '隐藏封面' : '显示封面', exact: true }).click();
    }
    if (width <= 900) await page.getByRole('button', { name: '聊天', exact: true }).click();
    const send = await page.getByRole('button', { name: '发送消息', exact: true }).boundingBox();
    const bar = await page.locator('.player-bar').boundingBox();
    expect(send.y + send.height).toBeLessThanOrEqual(bar.y);
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

test('点歌成功提示在弹窗上方，拒绝点歌只显示失败', async ({ page, request }) => {
  const id = await create(request, '点歌反馈');
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await expect(page.locator('.toast.ok')).toContainText('点歌成功');
  await expect(page.locator('.toasts')).toHaveJSProperty('popover', 'manual');
  expect(await page.locator('.toasts').evaluate((el) => el.matches(':popover-open'))).toBe(true);
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '房间设置', exact: true }).click();
  await page.getByPlaceholder('口令', { exact: true }).fill('test-room-owner');
  await page.getByRole('button', { name: '认证', exact: true }).click();
  await expect(page.getByRole('button', { name: '禁止点歌', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: '禁止点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.reload(); // a fresh listener session without global credentials
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await expect(page.locator('.toast.error')).toContainText('禁止点歌');
  await expect(page.locator('.toast.ok')).toHaveCount(0);
});

test('按用户昵称找到公开歌单并逐首点歌', async ({ page, request }) => {
  const id = await create(request, '用户歌单');
  await joined(page, id);
  await page.getByRole('button', { name: '添加歌曲', exact: true }).click();
  await page.getByRole('button', { name: '导入歌单', exact: true }).click();
  await page.getByRole('button', { name: '搜用户昵称', exact: true }).click();
  await page.getByPlaceholder('网易云用户昵称').fill('演示');
  await page.getByRole('button', { name: '搜索歌单', exact: true }).click();
  await page.getByRole('button', { name: /演示听众/ }).click();
  await page.locator('.playlists .pl').first().click();
  const song = await page.locator('.tracks .t-name').nth(2).textContent();
  await page
    .locator('.tracks .track')
    .nth(2)
    .getByRole('button', { name: '点歌', exact: true })
    .click();
  await expect(page.locator('.toast.ok').last()).toContainText('点歌成功');
  await expect(page.locator('.tracks .track').nth(2).getByRole('button')).toHaveText('已加入');
  await page.keyboard.press('Escape');
  await expect(page.locator('.song-info h1')).toHaveText(song);
});

test('创建者自动成为房主，修改管理密码后其他听众可认证', async ({ page, browser }) => {
  await page.goto('/');
  await page.getByRole('button', { name: '创建房间', exact: true }).click();
  await page.getByPlaceholder('例如：修图的时候听什么').fill('独立房主测试');
  await page.getByRole('button', { name: '创建并进入', exact: true }).click();
  await page.waitForURL('**/room/**');
  await page.getByRole('button', { name: '房间设置', exact: true }).click();
  await expect(page.getByRole('button', { name: '清空待播', exact: true })).toBeEnabled();
  await page.getByPlaceholder('新的房间管理密码').fill('new-local-owner');
  await page.getByRole('button', { name: '修改密码', exact: true }).click();
  await expect(page.locator('.toast.ok').last()).toContainText('管理密码已修改');
  const ctx = await browser.newContext();
  const guest = await ctx.newPage();
  await guest.goto(page.url());
  await guest.getByRole('button', { name: '房间设置', exact: true }).click();
  await expect(guest.getByRole('button', { name: '清空待播', exact: true })).toBeDisabled();
  await guest.getByPlaceholder('口令', { exact: true }).fill('new-local-owner');
  await guest.getByRole('button', { name: '认证', exact: true }).click();
  await expect(guest.getByRole('button', { name: '清空待播', exact: true })).toBeEnabled();
  await ctx.close();
});

test('听众可投票删除待播歌曲，当前歌曲保持播放', async ({ page, request }) => {
  const id = await create(request, '投票删除');
  await joined(page, id);
  await search(page, '恋');
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await expect(page.locator('.toast.ok').last()).toContainText('点歌成功');
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('竹');
  await page.getByRole('button', { name: '搜索音乐', exact: true }).click();
  await expect(page.locator('.t-name').first()).not.toHaveText('恋之瞳');
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: '投票删除 0/1', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '投票删除 0/1', exact: true }).click();
  await expect(page.locator('.queue-scroll li')).toHaveCount(1);
  await expect(page.locator('.song-info h1')).toHaveText('恋之瞳');
  await expect(page.locator('.toast.ok').last()).toContainText('投票通过');
});

test('无报错的播放停滞会自动重载，手动暂停不触发重载', async ({ page, request }) => {
  const id = await create(request, '停滞恢复');
  await observeAudio(page);
  await joined(page, id);
  await search(page);
  await page.locator('.track').first().getByRole('button', { name: '点歌', exact: true }).click();
  await page.keyboard.press('Escape');
  await ensurePlaying(page);
  await expect.poll(() => page.evaluate(() => window.__testAudio.currentTime)).toBeGreaterThan(0.2);
  await page.clock.install();
  await page.evaluate(() => {
    window.__audioLoads = 0;
    const a = window.__testAudio;
    const load = a.load.bind(a);
    a.load = () => {
      window.__audioLoads++;
      return load();
    };
    // Freeze observed progress without treating an OS audio-focus pause as a network failure.
    const time = a.currentTime;
    Object.defineProperty(a, 'currentTime', { configurable: true, get: () => time });
    document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player').checkProgress();
    a.dispatchEvent(new Event('waiting'));
  });
  await page.clock.fastForward(14000);
  await expect.poll(() => page.evaluate(() => window.__audioLoads)).toBeGreaterThan(0);
  await expect.poll(() => page.evaluate(() => window.__testAudio.paused)).toBe(false);
  await page.evaluate(() => {
    delete window.__testAudio.currentTime;
    document
      .querySelector('#app')
      .__vue_app__.config.globalProperties.$pinia._s.get('player')
      .pause();
    window.__audioLoads = 0;
  });
  await page.clock.fastForward(20000);
  expect(await page.evaluate(() => window.__audioLoads)).toBe(0);
  expect(await page.evaluate(() => window.__testAudio.paused)).toBe(true);
});

test('下一首提前下载，切歌复用已准备的播放器', async ({page, request}) => {
  const id = await create(request, '预加载切换');
  await observeAudio(page);
  const audioRequests = [];
  page.on('request', r => { if (r.url().includes('/api/demo/audio/')) audioRequests.push(r.url()); });
  await joined(page, id);
  await search(page, '恋');
  await page.locator('.track').first().getByRole('button', {name:'点歌',exact:true}).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('竹');
  await page.getByRole('button', {name:'搜索音乐',exact:true}).click();
  await expect(page.locator('.t-name').first()).not.toHaveText('恋之瞳');
  const nextName = await page.locator('.t-name').first().textContent();
  await page.locator('.track').first().getByRole('button', {name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');
  await ensurePlaying(page);
  await expect.poll(() => audioRequests.length).toBeGreaterThan(1);
  await expect.poll(() => page.evaluate(() => window.__testAudioInstances.some(a => a !== window.__testAudio && a.readyState >= 3))).toBe(true);
  await page.evaluate(() => { window.__beforeSwitchMedia = window.__testAudio; });
  const before = audioRequests.length;
  await page.getByTitle('投票切换到下一首', {exact:true}).click();
  await expect(page.locator('.song-info h1')).toHaveText(nextName);
  await expect.poll(() => page.evaluate(() => window.__testAudio !== window.__beforeSwitchMedia && !window.__testAudio.paused)).toBe(true);
  expect(audioRequests.length).toBe(before);
});

test('预加载坏节点在切歌前完成回退，切歌沿用验证成功的地址', async ({page, request}) => {
  const sample = await request.get('/api/demo/audio/d19.wav');
  const body = await sample.body();
  const hosts = [];
  await page.route('https://m704.music.126.net/**', async route => { hosts.push('m704'); await route.fulfill({status:403,body:'Forbidden'}); });
  await page.route('https://m701.music.126.net/**', async route => { hosts.push('m701'); await route.fulfill({status:200,contentType:'audio/wav',body}); });
  await observeAudio(page);
  const id = await create(request, '预加载节点回退');
  await joined(page,id);
  await search(page);
  await page.locator('.track').first().getByRole('button', {name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');
  await ensurePlaying(page);
  await page.evaluate(() => {
    window.__preparedTrack = {id:'standby',queueId:'standby',source:'wy',name:'standby',artist:'',album:'',cover:'',duration:32,fee:0,url:'https://m704.music.126.net/20990101/signature/jdymusic/obj/standby.mp3'};
    document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player').prepareNext(window.__preparedTrack);
  });
  await expect.poll(() => page.evaluate(() => window.__testAudioInstances.some(a => a !== window.__testAudio && a.readyState >= 3 && a.src.includes('m701')))).toBe(true);
  await page.evaluate(() => {
    const p = document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player');
    p.load(window.__preparedTrack,Date.now(),0,true);
  });
  await expect.poll(() => page.evaluate(() => window.__testAudio.currentTime)).toBeGreaterThan(0.1);
  expect(hosts).toEqual(['m704','m701']);
  expect(await page.evaluate(() => window.__testAudio.src)).toContain('m701.music.126.net');
});

test('迟到的播放地址刷新响应不会打断用户暂停', async ({page,request}) => {
  const id = await create(request,'刷新暂停边界');
  await observeAudio(page); await joined(page,id); await search(page);
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');await ensurePlaying(page);
  await page.evaluate(() => {
    const p=document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player');
    p.setRefreshHandler(track => new Promise(resolve => { window.__finishRefresh=()=>resolve({...track,url:track.url+'?late=1'}); }));
    p.recover();
  });
  await expect.poll(() => page.evaluate(() => typeof window.__finishRefresh)).toBe('function');
  await page.getByRole('button',{name:'暂停收听',exact:true}).click();
  await page.evaluate(() => window.__finishRefresh());
  await page.waitForTimeout(500);
  expect(await page.evaluate(() => window.__testAudio.paused)).toBe(true);
  expect(await page.evaluate(() => window.__testAudio.src)).not.toContain('late=1');
});

test('下一首先于首曲就绪时保留备用播放器', async ({page,request}) => {
  const id=await create(request,'预加载响应顺序');
  await observeAudio(page);await joined(page,id);
  await page.getByRole('button',{name:'添加歌曲',exact:true}).click();await page.keyboard.press('Escape');
  await page.evaluate(()=>{
    window.__earlyNext={id:'early',queueId:'early',source:'demo',name:'early',artist:'',album:'',cover:'',duration:32,fee:0,url:'/api/demo/audio/d01.wav'};
    const p=document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player');
    p.prepareNext(window.__earlyNext);
  });
  await expect.poll(()=>page.evaluate(()=>window.__testAudioInstances.some(a=>a!==window.__testAudio&&a.readyState>=3))).toBe(true);
  await page.evaluate(()=>{
    window.__earlyMedia=window.__testAudioInstances.find(a=>a!==window.__testAudio&&a.readyState>=3);
    const p=document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player');
    p.load({...window.__earlyNext,id:'initial',queueId:'initial',url:'/api/demo/audio/d19.wav'},Date.now(),0,true);
  });
  await ensurePlaying(page);
  expect(await page.evaluate(()=>window.__earlyMedia.readyState)).toBeGreaterThanOrEqual(3);
  await page.evaluate(()=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player').load(window.__earlyNext,Date.now(),0,true));
  await expect.poll(()=>page.evaluate(()=>window.__testAudio===window.__earlyMedia&&!window.__testAudio.paused)).toBe(true);
});

test('下一首的非预设 CDN 失败时提前重新取址', async ({page,request}) => {
  const id=await create(request,'预加载重新取址');
  await observeAudio(page);await joined(page,id);
  await page.route('https://source.invalid/**',route=>route.fulfill({status:503,body:'unavailable'}));
  await page.getByRole('button',{name:'添加歌曲',exact:true}).click();await page.keyboard.press('Escape');
  await page.evaluate(()=>{
    const p=document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player');
    window.__refreshCount=0;
    p.setRefreshHandler(async track=>{window.__refreshCount++;return {...track,url:'/api/demo/audio/d01.wav'}});
    window.__freshNext={id:'fresh-next',queueId:'fresh-next',source:'demo',name:'fresh-next',artist:'',album:'',cover:'',duration:32,fee:0,url:'https://source.invalid/song.mp3'};
    p.prepareNext(window.__freshNext);
  });
  await expect.poll(()=>page.evaluate(()=>window.__testAudioInstances.some(a=>a!==window.__testAudio&&a.readyState>=3))).toBe(true);
  expect(await page.evaluate(()=>window.__refreshCount)).toBe(1);
  await page.evaluate(()=>{
    window.__freshMedia=window.__testAudioInstances.find(a=>a!==window.__testAudio&&a.readyState>=3);
    document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('player').load(window.__freshNext,Date.now(),0,true);
  });
  await expect.poll(()=>page.evaluate(()=>window.__testAudio===window.__freshMedia&&!window.__testAudio.paused)).toBe(true);
  expect(await page.evaluate(()=>window.__testAudio.src)).toContain('/api/demo/audio/d01.wav');
});

async function mockSystemMedia(page) {
  await page.addInitScript(() => {
    window.__systemActions = {};
    window.__systemPositions = [];
    Object.defineProperty(navigator, 'mediaSession', {configurable:true,value:{
      metadata:null, playbackState:'none',
      setActionHandler(action, handler) { window.__systemActions[action] = handler; },
      setPositionState(state) { window.__systemPositions.push(state || null); },
    }});
    Object.defineProperty(navigator, 'audioSession', {configurable:true,value:{type:'auto'}});
  });
}
async function simulateVisibility(page, hidden) {
  await page.evaluate(hidden => {
    window.__background = hidden;
    Object.defineProperty(document, 'hidden', {configurable:true,get:()=>window.__background});
    Object.defineProperty(document, 'visibilityState', {configurable:true,get:()=>window.__background?'hidden':'visible'});
    document.dispatchEvent(new Event('visibilitychange'));
  }, hidden);
}

test('系统媒体会话显示歌曲，支持暂停、继续和投票切歌，离房清理', async ({page,request}) => {
  await mockSystemMedia(page);await observeAudio(page);
  const id=await create(request,'系统媒体会话');await joined(page,id);await search(page,'恋');
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('竹');
  await page.getByRole('button',{name:'搜索音乐',exact:true}).click();
  await expect(page.locator('.t-name').first()).not.toHaveText('恋之瞳');
  const nextName=await page.locator('.t-name').first().textContent();
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');await ensurePlaying(page);
  await expect.poll(()=>page.evaluate(()=>navigator.mediaSession.metadata?.title)).toBe('恋之瞳');
  await expect.poll(()=>page.evaluate(()=>navigator.mediaSession.playbackState)).toBe('playing');
  expect(await page.evaluate(()=>navigator.audioSession.type)).toBe('playback');
  expect(await page.evaluate(()=>window.__systemActions.seekto)).toBeNull();
  await page.evaluate(()=>window.__systemActions.pause());
  expect(await page.evaluate(()=>window.__testAudio.paused)).toBe(true);
  expect(await page.evaluate(()=>navigator.mediaSession.playbackState)).toBe('paused');
  await page.evaluate(()=>window.__systemActions.play());
  await expect.poll(()=>page.evaluate(()=>window.__testAudio.paused)).toBe(false);
  await page.evaluate(()=>window.__systemActions.nexttrack());
  await expect(page.locator('.song-info h1')).toHaveText(nextName);
  await expect.poll(()=>page.evaluate(()=>navigator.mediaSession.metadata?.title)).toBe(nextName);
  await page.getByRole('link',{name:'返回听歌大厅',exact:true}).click();
  await expect.poll(()=>page.evaluate(()=>navigator.mediaSession.playbackState)).toBe('none');
  expect(await page.evaluate(()=>navigator.mediaSession.metadata)).toBeNull();
  expect(await page.evaluate(()=>window.__systemActions.play)).toBeNull();
});

test('后台定时器延迟不会重载音频，后台换歌保持原播放元素', async ({page,request}) => {
  await observeAudio(page);const id=await create(request,'后台连续播放');
  await joined(page,id);await search(page,'恋');
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('竹');
  await page.getByRole('button',{name:'搜索音乐',exact:true}).click();
  await expect(page.locator('.t-name').first()).not.toHaveText('恋之瞳');
  const nextName=await page.locator('.t-name').first().textContent();
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');await ensurePlaying(page);
  await page.evaluate(()=>{
    window.__backgroundMedia=window.__testAudio;window.__backgroundLoads=0;
    const load=window.__testAudio.load.bind(window.__testAudio);
    window.__testAudio.load=()=>{window.__backgroundLoads++;load()};
  });
  await simulateVisibility(page,true);
  await page.clock.install();await page.clock.fastForward(15000);
  expect(await page.evaluate(()=>window.__backgroundLoads)).toBe(0);
  expect(await page.evaluate(()=>window.__testAudio.paused)).toBe(false);
  await page.evaluate(()=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('room').voteSkip());
  await expect(page.locator('.song-info h1')).toHaveText(nextName);
  await expect.poll(()=>page.evaluate(()=>window.__testAudio.paused)).toBe(false);
  expect(await page.evaluate(()=>window.__testAudio===window.__backgroundMedia)).toBe(true);
  await simulateVisibility(page,false);
  expect(await page.evaluate(()=>window.__testAudio===window.__backgroundMedia)).toBe(true);
});

test('系统暂停后切歌和恢复前台都不会擅自播放', async ({page,request}) => {
  await observeAudio(page);const id=await create(request,'系统中断');
  await joined(page,id);await search(page,'恋');
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.getByPlaceholder('歌名 / 歌手 / 专辑').fill('竹');
  await page.getByRole('button',{name:'搜索音乐',exact:true}).click();
  await expect(page.locator('.t-name').first()).not.toHaveText('恋之瞳');
  const nextName=await page.locator('.t-name').first().textContent();
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');await ensurePlaying(page);
  await expect.poll(()=>page.evaluate(()=>window.__testAudio.readyState)).toBeGreaterThanOrEqual(3);
  await page.evaluate(()=>window.__testAudio.pause());
  await expect(page.getByText('播放被系统中断，点击继续收听。')).toBeVisible();
  await page.evaluate(()=>window.__testAudio.play());
  await expect(page.getByText('播放被系统中断，点击继续收听。')).toHaveCount(0);
  await page.evaluate(()=>window.__testAudio.pause());
  await expect(page.getByText('播放被系统中断，点击继续收听。')).toBeVisible();
  await simulateVisibility(page,true);await simulateVisibility(page,false);
  await page.evaluate(()=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('room').voteSkip());
  await expect(page.locator('.song-info h1')).toHaveText(nextName);
  expect(await page.evaluate(()=>window.__testAudio.paused)).toBe(true);
  await page.getByRole('button',{name:'继续收听',exact:true}).click();
  await expect.poll(()=>page.evaluate(()=>window.__testAudio.paused)).toBe(false);
});

test('前台恢复会重连假活连接，保持音频和房间时间轴', async ({page,request}) => {
  let connections=0;let poison=false;
  await page.routeWebSocket(/\/api\/ws\?/,ws=>{
    const number=++connections;const server=ws.connectToServer();
    ws.onMessage(message=>{if(!(poison&&number===1))server.send(message)});
    server.onMessage(message=>{if(!(poison&&number===1))ws.send(message)});
  });
  await observeAudio(page);const id=await create(request,'后台连接恢复');
  await joined(page,id);await search(page);
  await page.locator('.track').first().getByRole('button',{name:'点歌',exact:true}).click();
  await page.keyboard.press('Escape');await ensurePlaying(page);
  const started=await page.evaluate(()=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('room').startedAt);
  await page.evaluate(()=>{window.__originalMedia=window.__testAudio});
  await simulateVisibility(page,true);poison=true;await simulateVisibility(page,false);
  await expect.poll(()=>connections,{timeout:8000}).toBeGreaterThan(1);
  await expect.poll(()=>page.evaluate(()=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('room').connected)).toBe(true);
  expect(await page.evaluate(()=>window.__testAudio===window.__originalMedia)).toBe(true);
  expect(await page.evaluate(()=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('room').startedAt)).toBe(started);
});
