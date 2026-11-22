<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { adminApi, type SaveStorageAccountBody } from '@/api/admin';
import BrandMark from '@/components/BrandMark.vue';
import Icon from '@/components/Icon.vue';
import SiteFooter from '@/components/SiteFooter.vue';
import { useUiStore } from '@/stores/ui';
import type {
  AdminConfigSnapshot,
  BiliAccountStatus,
  NeteaseAccountStatus,
  QRCreateResp,
} from '@/types';

const ui = useUiStore();

const checking = ref(true);
const authenticated = ref(false);
const loginPwd = ref('');
const loggingIn = ref(false);
const loginErr = ref('');

const cfg = ref<AdminConfigSnapshot | null>(null);
const loadingCfg = ref(false);

/* ——— 网易云状态 & 扫码 ——— */
const wyStatus = ref<NeteaseAccountStatus | null>(null);
const wyStatusLoading = ref(false);
const wyCookieInput = ref('');
const wySaving = ref(false);
const wyQR = ref<QRCreateResp | null>(null);
const wyQRState = ref('');
const wyQRHint = ref('');
let wyPollTimer = 0;
let wyQRRequest: AbortController | undefined;

/* ——— B 站状态 & 扫码 ——— */
const biliStatus = ref<BiliAccountStatus | null>(null);
const biliStatusLoading = ref(false);
const biliEnabled = ref(true);
const biliUseLogin = ref(true);
const biliCookieInput = ref('');
const biliSaving = ref(false);
const biliQR = ref<QRCreateResp | null>(null);
const biliQRState = ref('');
const biliQRHint = ref('');
let biliPollTimer = 0;
let biliQRRequest: AbortController | undefined;

/* ——— 对象存储 ——— */
interface StorageDraftAccount {
  existingIndex?: number;
  label: string;
  bucket: string;
  publicBase: string;
  accessKey: string;
  secretKey: string;
  accessKeyMasked: string;
  secretKeyMasked: string;
}
const storageForm = reactive({
  enabled: false,
  maxMBPerAccount: 10240,
  evictAfterDays: 0,
  minHits: 2,
  accounts: [] as StorageDraftAccount[],
});
const storageSaving = ref(false);

/* ——— 自定义 Key ——— */
const newKeyName = ref('');
const newKeyVal = ref('');
const keySaving = ref(false);

/* ——— 服务器口令 ——— */
const authForm = reactive({
  rootPassword: '',
  adminPassword: '',
});
const authSaving = ref(false);

function stopWyPoll() {
  wyQRRequest?.abort();
  wyQRRequest = undefined;
  if (wyPollTimer) {
    window.clearTimeout(wyPollTimer);
    wyPollTimer = 0;
  }
}

function stopBiliPoll() {
  biliQRRequest?.abort();
  biliQRRequest = undefined;
  if (biliPollTimer) {
    window.clearTimeout(biliPollTimer);
    biliPollTimer = 0;
  }
}

function applyConfig(snap: AdminConfigSnapshot) {
  cfg.value = snap;
  biliEnabled.value = snap.bilibili.enabled;
  biliUseLogin.value = snap.bilibili.useLogin;
  storageForm.enabled = snap.storage.enabled;
  storageForm.maxMBPerAccount = snap.storage.maxMBPerAccount;
  storageForm.evictAfterDays = snap.storage.evictAfterDays;
  storageForm.minHits = snap.storage.minHits;
  storageForm.accounts = snap.storage.accounts.map((a) => ({
    existingIndex: a.index,
    label: a.label,
    bucket: a.bucket,
    publicBase: a.publicBase,
    accessKey: '',
    secretKey: '',
    accessKeyMasked: a.accessKeyMasked,
    secretKeyMasked: a.secretKeyMasked,
  }));
}

async function loadAll() {
  loadingCfg.value = true;
  try {
    const snap = await adminApi.config();
    applyConfig(snap);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '加载后台配置失败');
  } finally {
    loadingCfg.value = false;
  }
}

async function checkSession() {
  checking.value = true;
  try {
    const s = await adminApi.session();
    authenticated.value = s.authenticated;
    if (s.authenticated) {
      await loadAll();
    }
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '检查登录状态失败');
  } finally {
    checking.value = false;
  }
}

async function login() {
  if (!loginPwd.value.trim()) {
    loginErr.value = '请输入服务器管理员口令';
    return;
  }
  loggingIn.value = true;
  loginErr.value = '';
  try {
    await adminApi.login(loginPwd.value);
    loginPwd.value = '';
    authenticated.value = true;
    ui.ok('欢迎回到管理后台');
    await loadAll();
  } catch (e) {
    loginErr.value = e instanceof Error ? e.message : '登录失败';
  } finally {
    loggingIn.value = false;
  }
}

async function logout() {
  stopWyPoll();
  stopBiliPoll();
  wyQR.value = null;
  biliQR.value = null;
  try {
    await adminApi.logout();
  } finally {
    authenticated.value = false;
    cfg.value = null;
    wyStatus.value = null;
    biliStatus.value = null;
    ui.ok('已退出管理后台');
  }
}

/* ——— 网易云操作 ——— */
async function refreshWyStatus() {
  wyStatusLoading.value = true;
  try {
    wyStatus.value = await adminApi.neteaseStatus();
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '检测网易云状态失败');
  } finally {
    wyStatusLoading.value = false;
  }
}

async function startWyQR() {
  stopWyPoll();
  const request = new AbortController();
  wyQRRequest = request;
  wyQR.value = null;
  wyQRState.value = 'loading';
  wyQRHint.value = '正在生成网易云二维码…';
  try {
    const qr = await adminApi.neteaseQRCreate(request.signal);
    if (request.signal.aborted) return;
    wyQR.value = qr;
    wyQRState.value = 'waiting';
    wyQRHint.value = '请使用网易云音乐 App 扫描二维码';
    wyPollTimer = window.setTimeout(() => void pollWyQR(qr.key, request), 2200);
  } catch (e) {
    if (request.signal.aborted) return;
    wyQRState.value = 'error';
    wyQRHint.value = e instanceof Error ? e.message : '生成二维码失败';
    ui.error(wyQRHint.value);
  }
}

async function pollWyQR(key: string, request: AbortController) {
  if (request.signal.aborted) return;
  try {
    const res = await adminApi.neteaseQRPoll(key, request.signal);
    if (request.signal.aborted) return;
    wyQRState.value = res.status;
    wyQRHint.value =
      res.status === 'scanned' && res.nickname
        ? `已扫码（${res.nickname}），请在手机端确认登录`
        : res.message;
    if (res.status === 'confirmed') {
      stopWyPoll();
      wyQR.value = null;
      if (res.config) applyConfig(res.config);
      ui.ok('网易云扫码登录成功，Cookie 已保存并生效');
      void refreshWyStatus();
    } else if (res.status === 'expired') {
      stopWyPoll();
    } else {
      wyPollTimer = window.setTimeout(() => void pollWyQR(key, request), 2200);
    }
  } catch (e) {
    if (request.signal.aborted) return;
    stopWyPoll();
    wyQRState.value = 'error';
    wyQRHint.value = e instanceof Error ? e.message : '轮询扫码状态失败';
  }
}

async function saveWyCookie(clear = false) {
  if (!clear && !wyCookieInput.value.trim()) {
    return ui.warn('请先粘贴网易云 Cookie');
  }
  stopWyPoll();
  wyQR.value = null;
  wyQRHint.value = '';
  wySaving.value = true;
  try {
    const res = await adminApi.saveNetease(clear ? { clear: true } : { cookie: wyCookieInput.value });
    applyConfig(res.config);
    wyCookieInput.value = '';
    wyStatus.value = null;
    ui.ok(clear ? '已清空网易云 Cookie' : '网易云 Cookie 已保存并热生效');
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '保存失败');
  } finally {
    wySaving.value = false;
  }
}

/* ——— B 站操作 ——— */
async function refreshBiliStatus() {
  biliStatusLoading.value = true;
  try {
    biliStatus.value = await adminApi.biliStatus();
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '检测 B 站状态失败');
  } finally {
    biliStatusLoading.value = false;
  }
}

async function startBiliQR() {
  stopBiliPoll();
  const request = new AbortController();
  biliQRRequest = request;
  biliQR.value = null;
  biliQRState.value = 'loading';
  biliQRHint.value = '正在生成 B 站二维码…';
  try {
    const qr = await adminApi.biliQRCreate(request.signal);
    if (request.signal.aborted) return;
    biliQR.value = qr;
    biliQRState.value = 'waiting';
    biliQRHint.value = '请使用哔哩哔哩 App 扫描二维码';
    biliPollTimer = window.setTimeout(() => void pollBiliQR(qr.key, request), 2200);
  } catch (e) {
    if (request.signal.aborted) return;
    biliQRState.value = 'error';
    biliQRHint.value = e instanceof Error ? e.message : '生成二维码失败';
    ui.error(biliQRHint.value);
  }
}

async function pollBiliQR(key: string, request: AbortController) {
  if (request.signal.aborted) return;
  try {
    const res = await adminApi.biliQRPoll(key, request.signal);
    if (request.signal.aborted) return;
    biliQRState.value = res.status;
    biliQRHint.value = res.message;
    if (res.status === 'confirmed') {
      stopBiliPoll();
      biliQR.value = null;
      if (res.config) applyConfig(res.config);
      ui.ok('B 站扫码登录成功，Cookie 已保存并生效');
      void refreshBiliStatus();
    } else if (res.status === 'expired') {
      stopBiliPoll();
    } else {
      biliPollTimer = window.setTimeout(() => void pollBiliQR(key, request), 2200);
    }
  } catch (e) {
    if (request.signal.aborted) return;
    stopBiliPoll();
    biliQRState.value = 'error';
    biliQRHint.value = e instanceof Error ? e.message : '轮询扫码状态失败';
  }
}

async function saveBiliConfig(clearCookie = false) {
  if (clearCookie || biliCookieInput.value.trim()) {
    stopBiliPoll();
    biliQR.value = null;
    biliQRHint.value = '';
  }
  biliSaving.value = true;
  try {
    const res = await adminApi.saveBilibili({
      enabled: biliEnabled.value,
      useLogin: biliUseLogin.value,
      cookie: clearCookie ? '' : biliCookieInput.value.trim(),
      clearCookie,
    });
    applyConfig(res.config);
    biliCookieInput.value = '';
    if (clearCookie) biliStatus.value = null;
    ui.ok(clearCookie ? '已清空 B 站 Cookie' : 'B 站配置已保存并热生效');
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '保存 B 站配置失败');
  } finally {
    biliSaving.value = false;
  }
}

/* ——— 对象存储操作 ——— */
function addStorageAccount() {
  storageForm.accounts.push({
    label: `acc-${storageForm.accounts.length + 1}`,
    bucket: '',
    publicBase: '',
    accessKey: '',
    secretKey: '',
    accessKeyMasked: '',
    secretKeyMasked: '',
  });
}

function removeStorageAccount(idx: number) {
  storageForm.accounts.splice(idx, 1);
}

async function saveStorage() {
  storageSaving.value = true;
  try {
    const accounts: SaveStorageAccountBody[] = storageForm.accounts.map((a) => ({
      ...(a.existingIndex !== undefined ? { existingIndex: a.existingIndex } : {}),
      label: a.label.trim(),
      bucket: a.bucket.trim(),
      publicBase: a.publicBase.trim(),
      accessKey: a.accessKey.trim(),
      secretKey: a.secretKey.trim(),
    }));
    const res = await adminApi.saveStorage({
      enabled: storageForm.enabled,
      maxMBPerAccount: Number(storageForm.maxMBPerAccount) || 0,
      evictAfterDays: Number(storageForm.evictAfterDays) || 0,
      minHits: Number(storageForm.minHits) || 0,
      accounts,
    });
    applyConfig(res.config);
    ui.ok('对象存储配置已保存并生效');
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '保存对象存储配置失败');
  } finally {
    storageSaving.value = false;
  }
}

/* ——— 自定义 Key 操作 ——— */
async function addCustomKey() {
  const k = newKeyName.value.trim();
  const v = newKeyVal.value.trim();
  if (!k || !v) return ui.warn('请填写 Key 名称与 Key 值');
  keySaving.value = true;
  try {
    const res = await adminApi.saveKeys({ set: { [k]: v } });
    applyConfig(res.config);
    newKeyName.value = '';
    newKeyVal.value = '';
    ui.ok(`已保存 Key：${k}`);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '保存 Key 失败');
  } finally {
    keySaving.value = false;
  }
}

async function deleteCustomKey(key: string) {
  keySaving.value = true;
  try {
    const res = await adminApi.saveKeys({ delete: [key] });
    applyConfig(res.config);
    ui.ok(`已删除 Key：${key}`);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '删除 Key 失败');
  } finally {
    keySaving.value = false;
  }
}

/* ——— 服务器口令 ——— */
async function saveAuthPasswords() {
  if (!authForm.rootPassword.trim() && !authForm.adminPassword.trim()) {
    return ui.warn('请至少填写一个新口令');
  }
  authSaving.value = true;
  try {
    const res = await adminApi.saveAuth({
      rootPassword: authForm.rootPassword.trim(),
      adminPassword: authForm.adminPassword.trim(),
    });
    applyConfig(res.config);
    authForm.rootPassword = '';
    authForm.adminPassword = '';
    ui.ok('服务器管理口令已更新');
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '修改口令失败');
  } finally {
    authSaving.value = false;
  }
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B';
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

onMounted(() => void checkSession());
onBeforeUnmount(() => {
  stopWyPoll();
  stopBiliPoll();
});
</script>

<template>
  <div class="admin-page">
    <header class="admin-header">
      <RouterLink to="/" class="brand" aria-label="返回茶与金平糖听歌大厅">
        <BrandMark />
        <span>
          <b>茶与金平糖<span class="brand-dot">.</span></b>
          <small>ADMIN / 管理后台</small>
        </span>
      </RouterLink>
      <div class="head-actions">
        <RouterLink class="btn btn-ghost btn-sm" to="/">
          <Icon name="home" :size="15" />返回大厅
        </RouterLink>
        <button v-if="authenticated" class="btn btn-ghost btn-sm" type="button" @click="logout">
          退出登录
        </button>
      </div>
    </header>

    <main class="admin-main">
      <div v-if="checking" class="gate-card">
        <p class="muted">正在检查管理会话…</p>
      </div>

      <!-- 登录门槛 -->
      <section v-else-if="!authenticated" class="gate-card" aria-label="管理员登录">
        <span class="gate-icon"><Icon name="lock" :size="26" /></span>
        <h1>管理员后台</h1>
        <p class="gate-sub">
          使用服务器超级管理员口令（<code>rootPassword</code>）登录，管理网易云 / B站登录态、Cookie 与各类密钥配置。
        </p>
        <form class="gate-form" @submit.prevent="login">
          <label class="field">
            <span>管理员口令</span>
            <input
              v-model="loginPwd"
              class="input"
              type="password"
              autocomplete="current-password"
              placeholder="输入服务器 rootPassword"
              required
            />
          </label>
          <p v-if="loginErr" class="err-box" role="alert">{{ loginErr }}</p>
          <button class="btn btn-primary" type="submit" :disabled="loggingIn">
            {{ loggingIn ? '正在验证…' : '登录管理后台' }}
          </button>
        </form>
      </section>

      <!-- 后台主界面 -->
      <div v-else class="dashboard">
        <section v-if="cfg" class="card summary-card" aria-label="运行概览">
          <div class="card-head">
            <div>
              <h2>运行状态与配置文件</h2>
              <p class="sub">
                所有在后台修改的 Cookie、Key 与口令会以 <code>0600</code> 权限原子落盘到独立密钥配置文件，并立即热生效。
              </p>
            </div>
            <button class="btn btn-ghost btn-sm" type="button" :disabled="loadingCfg" @click="loadAll">
              <Icon name="refresh" :size="14" />刷新
            </button>
          </div>
          <div class="meta-grid">
            <div class="meta-item">
              <span>当前主音源</span>
              <b>{{ cfg.primaryProvider === 'wy' ? '网易云音乐 (wy)' : '演示电台 (demo)' }}</b>
            </div>
            <div class="meta-item">
              <span>已启用音源</span>
              <b>{{ cfg.sources.map((s) => s.label).join(' · ') || '无' }}</b>
            </div>
            <div class="meta-item">
              <span>密钥配置文件</span>
              <code>{{ cfg.secretsFile }}</code>
              <small :class="cfg.secretsWritable ? 'ok-text' : 'warn-text'">
                {{ cfg.secretsWritable ? '可写（支持页面落盘）' : '只读（请检查目录权限）' }}
              </small>
            </div>
            <div class="meta-item">
              <span>服务版本</span>
              <b>{{ cfg.version }}</b>
              <small v-if="cfg.configFiles.length" class="muted">
                主配置：{{ cfg.configFiles.join(', ') }}
              </small>
            </div>
          </div>
          <p v-if="cfg.envOverrides.length" class="notice-banner">
            当前检测到环境变量覆盖：<code>{{ cfg.envOverrides.join(', ') }}</code>（环境变量优先级高于配置文件，重启后会以环境变量为准）。
          </p>
        </section>

        <!-- 音乐平台账号：网易云 & B站 -->
        <div v-if="cfg" class="grid-two">
          <!-- 网易云音乐 -->
          <section class="card" aria-label="网易云音乐账号">
            <div class="card-head">
              <div>
                <h2>网易云音乐</h2>
                <p class="sub">扫码登录或显式配置 Cookie，支持 320k 与 VIP 曲目解析。</p>
              </div>
              <span class="tag" :class="cfg.netease.configured ? '' : 'gold'">
                {{ cfg.netease.configured ? '已配置 Cookie' : '未配置' }}
              </span>
            </div>

            <div class="status-box">
              <div class="status-row">
                <div v-if="wyStatus" class="acct-info">
                  <img v-if="wyStatus.avatar" :src="wyStatus.avatar" alt="" class="acct-avatar" />
                  <div>
                    <b>{{ wyStatus.loggedIn ? wyStatus.nickname : '未登录 / 已失效' }}</b>
                    <small>
                      <template v-if="wyStatus.loggedIn">
                        UID {{ wyStatus.uid }}
                        <template v-if="wyStatus.vipType"> · VIP</template>
                      </template>
                      <template v-else>{{ wyStatus.message }}</template>
                    </small>
                  </div>
                </div>
                <div v-else class="acct-info">
                  <div>
                    <b>{{ cfg.netease.configured ? '已配置凭据' : '尚未登录网易云' }}</b>
                    <small v-if="cfg.netease.cookieKeys.length">
                      包含键：{{ cfg.netease.cookieKeys.join(', ') }}
                    </small>
                    <small v-else>可点击下方扫码登录，或手动粘贴 Cookie。</small>
                  </div>
                </div>
                <button
                  class="btn btn-ghost btn-sm"
                  type="button"
                  :disabled="wyStatusLoading"
                  @click="refreshWyStatus"
                >
                  {{ wyStatusLoading ? '检测中…' : '检测登录态' }}
                </button>
              </div>
            </div>

            <div class="qr-area">
              <div class="qr-actions">
                <button class="btn btn-primary btn-sm" type="button" @click="startWyQR">
                  {{ wyQR ? '刷新网易云二维码' : '扫码登录网易云' }}
                </button>
                <button
                  v-if="wyQR"
                  class="btn btn-ghost btn-sm"
                  type="button"
                  @click="
                    stopWyPoll();
                    wyQR = null;
                    wyQRHint = '';
                  "
                >
                  取消扫码
                </button>
              </div>
              <div v-if="wyQR" class="qr-preview">
                <img :src="wyQR.qrSvgDataUrl" alt="网易云登录二维码" class="qr-img" />
                <p class="qr-hint" :class="{ expired: wyQRState === 'expired' }">{{ wyQRHint }}</p>
              </div>
              <p v-else-if="wyQRHint" class="qr-hint">{{ wyQRHint }}</p>
            </div>

            <form class="cookie-form" @submit.prevent="saveWyCookie(false)">
              <label class="field">
                <span>显式配置网易云 Cookie</span>
                <textarea
                  v-model="wyCookieInput"
                  class="input textarea"
                  rows="3"
                  placeholder="粘贴 MUSIC_U=...; __csrf=...（留空不改）"
                />
              </label>
              <div class="form-actions">
                <button class="btn btn-primary btn-sm" type="submit" :disabled="wySaving">
                  保存 Cookie
                </button>
                <button
                  v-if="cfg.netease.configured"
                  class="btn btn-danger btn-sm"
                  type="button"
                  :disabled="wySaving"
                  @click="saveWyCookie(true)"
                >
                  清空 Cookie
                </button>
              </div>
            </form>
          </section>

          <!-- 哔哩哔哩（B站） -->
          <section class="card" aria-label="哔哩哔哩账号与配置">
            <div class="card-head">
              <div>
                <h2>哔哩哔哩（B站）</h2>
                <p class="sub">搜视频并仅抽纯音频播放（DASH AAC）；默认使用登录态并显式配置 Cookie。</p>
              </div>
              <span class="tag" :class="cfg.bilibili.configured ? '' : 'gold'">
                {{ cfg.bilibili.configured ? '已配置 Cookie' : '未配置' }}
              </span>
            </div>

            <div class="status-box">
              <div class="status-row">
                <div v-if="biliStatus" class="acct-info">
                  <img v-if="biliStatus.face" :src="biliStatus.face" alt="" class="acct-avatar" />
                  <div>
                    <b>{{ biliStatus.loggedIn ? biliStatus.username : '未登录 / 已失效' }}</b>
                    <small>
                      <template v-if="biliStatus.loggedIn">
                        UID {{ biliStatus.uid }}
                        <template v-if="biliStatus.vipLabel"> · {{ biliStatus.vipLabel }}</template>
                      </template>
                      <template v-else>{{ biliStatus.message }}</template>
                    </small>
                  </div>
                </div>
                <div v-else class="acct-info">
                  <div>
                    <b>{{ cfg.bilibili.configured ? '已配置显式 Cookie' : '尚未登录 B 站' }}</b>
                    <small v-if="cfg.bilibili.cookieKeys.length">
                      包含键：{{ cfg.bilibili.cookieKeys.join(', ') }}
                    </small>
                    <small v-else>默认要求登录态，建议扫码登录或粘贴含 SESSDATA 的 Cookie。</small>
                  </div>
                </div>
                <button
                  class="btn btn-ghost btn-sm"
                  type="button"
                  :disabled="biliStatusLoading"
                  @click="refreshBiliStatus"
                >
                  {{ biliStatusLoading ? '检测中…' : '检测登录态' }}
                </button>
              </div>
            </div>

            <div class="toggle-list">
              <label class="toggle-item">
                <input v-model="biliEnabled" type="checkbox" />
                <span>
                  <b>启用 B 站搜歌与抽音</b>
                  <small>开启后房间「搜索点歌」显示哔哩哔哩音源。</small>
                </span>
              </label>
              <label class="toggle-item">
                <input v-model="biliUseLogin" type="checkbox" />
                <span>
                  <b>默认使用登录态</b>
                  <small>开启后强制校验显式登录 Cookie（SESSDATA），避免匿名风控与低码率。</small>
                </span>
              </label>
            </div>

            <div class="qr-area">
              <div class="qr-actions">
                <button class="btn btn-primary btn-sm" type="button" @click="startBiliQR">
                  {{ biliQR ? '刷新 B 站二维码' : '扫码登录 B 站' }}
                </button>
                <button
                  v-if="biliQR"
                  class="btn btn-ghost btn-sm"
                  type="button"
                  @click="
                    stopBiliPoll();
                    biliQR = null;
                    biliQRHint = '';
                  "
                >
                  取消扫码
                </button>
              </div>
              <div v-if="biliQR" class="qr-preview">
                <img :src="biliQR.qrSvgDataUrl" alt="B站登录二维码" class="qr-img" />
                <p class="qr-hint" :class="{ expired: biliQRState === 'expired' }">{{ biliQRHint }}</p>
              </div>
              <p v-else-if="biliQRHint" class="qr-hint">{{ biliQRHint }}</p>
            </div>

            <form class="cookie-form" @submit.prevent="saveBiliConfig(false)">
              <label class="field">
                <span>显式配置 B 站 Cookie</span>
                <textarea
                  v-model="biliCookieInput"
                  class="input textarea"
                  rows="3"
                  placeholder="粘贴 SESSDATA=...; bili_jct=...; DedeUserID=...（留空则仅保存上方开关）"
                />
              </label>
              <div class="form-actions">
                <button class="btn btn-primary btn-sm" type="submit" :disabled="biliSaving">
                  保存 B 站配置
                </button>
                <button
                  v-if="cfg.bilibili.configured"
                  class="btn btn-danger btn-sm"
                  type="button"
                  :disabled="biliSaving"
                  @click="saveBiliConfig(true)"
                >
                  清空 Cookie
                </button>
              </div>
            </form>
          </section>
        </div>

        <!-- 对象存储（多吉云）与 Key -->
        <section v-if="cfg" class="card" aria-label="对象存储与音频缓存配置">
          <div class="card-head">
            <div>
              <h2>对象存储（多吉云音频缓存）</h2>
              <p class="sub">
                网易云与 B 站听过的音频均可异步落桶，命中后 302 直连 CDN/COS。
              </p>
            </div>
            <span class="tag" :class="cfg.cacheStats.enabled ? '' : 'gold'">
              {{
                cfg.cacheStats.enabled
                  ? `已缓存 ${cfg.cacheStats.items || 0} 首 · 命中 ${cfg.cacheStats.hits || 0} 次`
                  : '缓存未启用'
              }}
            </span>
          </div>

          <p class="notice-banner">
            注意：缓存索引按账号序号（#0、#1…）引用存储桶，缓存启用或已有索引时，可在末尾添加账号。已有账号须保留序号、<code>AccessKey</code> 和存储空间；编辑时密钥留空会沿用已保存的值。
          </p>

          <div v-if="cfg.cacheStats.enabled && cfg.cacheStats.accounts?.length" class="cache-strip">
            <span v-for="(acc, i) in cfg.cacheStats.accounts" :key="i" class="cache-pill">
              #{{ i }} {{ acc.label || acc.bucket }}：{{ acc.items }} 首 / {{ formatBytes(acc.bytes) }}
            </span>
          </div>

          <form class="storage-form" @submit.prevent="saveStorage">
            <div class="storage-globals">
              <label class="toggle-item inline-toggle">
                <input v-model="storageForm.enabled" type="checkbox" />
                <span><b>启用对象存储音频缓存</b></span>
              </label>
              <label class="field">
                <span>单账号上限 (MiB，0=不限)</span>
                <input v-model.number="storageForm.maxMBPerAccount" class="input" type="number" min="0" />
              </label>
              <label class="field">
                <span>冷门淘汰天数 (0=不淘汰)</span>
                <input v-model.number="storageForm.evictAfterDays" class="input" type="number" min="0" />
              </label>
              <label class="field">
                <span>保护命中阈值 (minHits)</span>
                <input v-model.number="storageForm.minHits" class="input" type="number" min="0" />
              </label>
            </div>

            <div class="account-list">
              <div v-for="(acc, i) in storageForm.accounts" :key="i" class="account-row">
                <div class="account-row-head">
                  <b>账号 #{{ i }}</b>
                  <span v-if="acc.existingIndex !== undefined" class="muted">
                    已存密钥：AK {{ acc.accessKeyMasked || '未设' }} / SK {{ acc.secretKeyMasked || '未设' }}
                  </span>
                  <button
                    class="link-danger"
                    type="button"
                    @click="removeStorageAccount(i)"
                  >
                    移除
                  </button>
                </div>
                <div class="account-fields">
                  <label class="field">
                    <span>名称 (label)</span>
                    <input v-model="acc.label" class="input" placeholder="如 acc-1" />
                  </label>
                  <label class="field">
                    <span>空间名 (bucket)</span>
                    <input v-model="acc.bucket" class="input" placeholder="如 tk-audio-a" />
                  </label>
                  <label class="field">
                    <span>加速域名 (publicBase，选填)</span>
                    <input v-model="acc.publicBase" class="input" placeholder="https://audio.example.com" />
                  </label>
                  <label class="field">
                    <span>AccessKey</span>
                    <input
                      v-model="acc.accessKey"
                      class="input"
                      :placeholder="acc.accessKeyMasked ? `留空保持 ${acc.accessKeyMasked}` : '输入 AccessKey'"
                    />
                  </label>
                  <label class="field">
                    <span>SecretKey</span>
                    <input
                      v-model="acc.secretKey"
                      class="input"
                      type="password"
                      autocomplete="new-password"
                      :placeholder="acc.secretKeyMasked ? `留空保持 ${acc.secretKeyMasked}` : '输入 SecretKey'"
                    />
                  </label>
                </div>
              </div>
            </div>

            <div class="form-actions">
              <button class="btn btn-ghost btn-sm" type="button" @click="addStorageAccount">
                <Icon name="plus" :size="14" />添加存储账号
              </button>
              <button class="btn btn-primary btn-sm" type="submit" :disabled="storageSaving">
                保存对象存储配置
              </button>
            </div>
          </form>
        </section>

        <!-- 自定义 Key 与管理员口令 -->
        <div v-if="cfg" class="grid-two">
          <section class="card" aria-label="自定义 Key 配置">
            <div class="card-head">
              <div>
                <h2>自定义 Key / 凭据</h2>
                <p class="sub">统一存放在 <code>secrets.json</code> 的 <code>keys</code> 字典中，页面只展示脱敏摘要。</p>
              </div>
            </div>

            <ul v-if="cfg.keys.length" class="key-list">
              <li v-for="item in cfg.keys" :key="item.key" class="key-row">
                <div>
                  <code>{{ item.key }}</code>
                  <small>{{ item.masked }}（{{ item.length }} 字符）</small>
                </div>
                <button
                  class="link-danger"
                  type="button"
                  :disabled="keySaving"
                  @click="deleteCustomKey(item.key)"
                >
                  删除
                </button>
              </li>
            </ul>
            <p v-else class="empty-note">尚未配置自定义 Key。</p>

            <form class="key-form" @submit.prevent="addCustomKey">
              <label class="field">
                <span>Key 名称</span>
                <input v-model="newKeyName" class="input" maxlength="64" placeholder="例如：lyricsApiKey" />
              </label>
              <label class="field">
                <span>Key 值</span>
                <input
                  v-model="newKeyVal"
                  class="input"
                  type="password"
                  autocomplete="new-password"
                  placeholder="输入密钥值"
                />
              </label>
              <div class="form-actions">
                <button class="btn btn-primary btn-sm" type="submit" :disabled="keySaving">
                  新增 / 更新 Key
                </button>
              </div>
            </form>
          </section>

          <section class="card" aria-label="服务器口令管理">
            <div class="card-head">
              <div>
                <h2>服务器管理口令</h2>
                <p class="sub">修改后立即生效并落盘到 <code>secrets.json</code>；留空表示保持当前口令不变。</p>
              </div>
            </div>

            <form class="key-form" @submit.prevent="saveAuthPasswords">
              <label class="field">
                <span>超级管理员口令（rootPassword）</span>
                <input
                  v-model="authForm.rootPassword"
                  class="input"
                  type="password"
                  maxlength="64"
                  autocomplete="new-password"
                  placeholder="用于登录管理后台与管理全部房间"
                />
              </label>
              <label class="field">
                <span>房间管理员口令（adminPassword）</span>
                <input
                  v-model="authForm.adminPassword"
                  class="input"
                  type="password"
                  maxlength="64"
                  autocomplete="new-password"
                  placeholder="用于房间内普通管理员认证"
                />
              </label>
              <div class="form-actions">
                <button class="btn btn-primary btn-sm" type="submit" :disabled="authSaving">
                  更新口令
                </button>
              </div>
            </form>
          </section>
        </div>
      </div>

      <SiteFooter />
    </main>
  </div>
</template>

<style scoped>
.admin-page {
  min-height: 100dvh;
  background: var(--paper);
}
.admin-header {
  height: 76px;
  max-width: 1240px;
  margin: 0 auto;
  padding: 0 28px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 11px;
}
.brand b {
  font-size: 19px;
  letter-spacing: -0.6px;
}
.brand-dot {
  color: var(--vermilion);
  padding-left: 2px;
}
.brand small {
  display: block;
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 1.4px;
  color: var(--muted);
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.admin-main {
  max-width: 1240px;
  margin: 0 auto;
  padding: 12px 28px 32px;
}
.gate-card {
  max-width: 440px;
  margin: 56px auto 72px;
  padding: 34px 30px;
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 20px;
  box-shadow: var(--shadow-card);
  text-align: center;
}
.gate-icon {
  width: 52px;
  height: 52px;
  margin: 0 auto 14px;
  border-radius: 14px;
  background: var(--sage);
  color: var(--green);
  display: grid;
  place-items: center;
}
.gate-card h1 {
  font-size: 22px;
}
.gate-sub {
  margin-top: 8px;
  font-size: 12px;
  color: var(--muted);
  line-height: 1.7;
}
.gate-form {
  margin-top: 22px;
  display: grid;
  gap: 14px;
  text-align: left;
}
.err-box {
  padding: 9px 12px;
  border-radius: 9px;
  background: var(--vermilion-soft);
  color: var(--vermilion);
  font-size: 12px;
}
.dashboard {
  display: grid;
  gap: 20px;
}
.grid-two {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  align-items: start;
}
.card {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 18px;
  padding: 22px 24px;
  box-shadow: var(--shadow-card);
}
.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 16px;
}
.card-head h2 {
  font-size: 17px;
}
.sub {
  margin-top: 4px;
  font-size: 12px;
  color: var(--muted);
}
.meta-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}
.meta-item {
  background: var(--paper-2);
  border-radius: 12px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.meta-item > span {
  font-size: 11px;
  color: var(--muted);
}
.meta-item > b,
.meta-item > code {
  font-size: 13px;
  overflow-wrap: anywhere;
}
.meta-item > small {
  font-size: 11px;
}
.ok-text {
  color: var(--green);
}
.warn-text {
  color: var(--vermilion);
}
.notice-banner {
  margin-top: 14px;
  padding: 10px 13px;
  border-radius: 10px;
  background: var(--gold-soft);
  color: #7c591a;
  font-size: 12px;
  line-height: 1.6;
}
.status-box {
  background: var(--paper-2);
  border-radius: 12px;
  padding: 12px 14px;
  margin-bottom: 14px;
}
.status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.acct-info {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.acct-info > div {
  min-width: 0;
}
.acct-info b {
  display: block;
  font-size: 13px;
}
.acct-info small {
  display: block;
  font-size: 11px;
  color: var(--muted);
  overflow-wrap: anywhere;
}
.acct-avatar {
  width: 38px;
  height: 38px;
  border-radius: 50%;
  object-fit: cover;
  flex: none;
}
.toggle-list {
  display: grid;
  gap: 10px;
  margin-bottom: 14px;
}
.toggle-item {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  font-size: 12px;
  accent-color: var(--green);
  cursor: pointer;
}
.toggle-item input {
  margin-top: 3px;
}
.toggle-item b {
  display: block;
  font-size: 13px;
}
.toggle-item small {
  display: block;
  color: var(--muted);
  font-size: 11px;
}
.qr-area {
  margin-bottom: 16px;
}
.qr-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.qr-preview {
  margin-top: 12px;
  padding: 14px;
  border-radius: 12px;
  border: 1px dashed var(--line-strong);
  background: var(--paper);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}
.qr-img {
  width: 184px;
  height: 184px;
  border-radius: 8px;
  background: #fff;
  padding: 4px;
  border: 1px solid var(--line);
}
.qr-hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--green);
}
.qr-hint.expired {
  color: var(--vermilion);
}
.cookie-form,
.key-form,
.storage-form {
  display: grid;
  gap: 12px;
}
.textarea {
  height: auto;
  padding: 10px 12px;
  font-family: var(--mono);
  font-size: 12px;
  resize: vertical;
}
.form-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.btn-danger {
  background: var(--vermilion-soft);
  color: var(--vermilion);
  border-color: #f0d4ce;
}
.btn-danger:hover {
  background: #f7dfd8;
}
.cache-strip {
  margin-top: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.cache-pill {
  padding: 5px 10px;
  border-radius: 8px;
  background: var(--sage);
  color: var(--green);
  font-size: 11px;
  font-weight: 600;
}
.storage-globals {
  margin-top: 14px;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  align-items: end;
}
.inline-toggle {
  align-items: center;
  min-height: 44px;
}
.account-list {
  display: grid;
  gap: 12px;
}
.account-row {
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 14px;
  background: var(--paper);
}
.account-row-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
  font-size: 12px;
}
.account-row-head .link-danger {
  margin-left: auto;
}
.account-fields {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.key-list {
  display: grid;
  gap: 8px;
  margin-bottom: 14px;
}
.key-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--paper-2);
}
.key-row code {
  display: block;
  font-weight: 600;
  font-size: 12px;
}
.key-row small {
  display: block;
  font-size: 11px;
  color: var(--muted);
}
.empty-note {
  font-size: 12px;
  color: var(--muted);
  margin-bottom: 12px;
}
.link-danger {
  font-size: 12px;
  color: var(--vermilion);
  padding: 4px 8px;
  border-radius: 6px;
}
.link-danger:hover {
  background: var(--vermilion-soft);
}
@media (max-width: 980px) {
  .grid-two,
  .meta-grid,
  .storage-globals,
  .account-fields {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 600px) {
  .admin-header,
  .admin-main {
    padding-left: 16px;
    padding-right: 16px;
  }
  .card {
    padding: 18px 16px;
  }
}
</style>
