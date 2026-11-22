import { ApiError } from '@/api/http';
import type {
  AdminConfigSnapshot,
  AdminSessionInfo,
  BiliAccountStatus,
  NeteaseAccountStatus,
  QRCreateResp,
  QRPollResp,
} from '@/types';

async function adminFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/admin${path}`, {
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
    },
    ...init,
  });
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string } } | null)?.error;
    throw new ApiError(
      err?.message || `请求失败（HTTP ${res.status}）`,
      err?.code || 'http_error',
      res.status,
    );
  }
  return data as T;
}

export interface SaveStorageAccountBody {
  existingIndex?: number;
  label: string;
  bucket: string;
  publicBase: string;
  accessKey?: string;
  secretKey?: string;
}

export const adminApi = {
  session: () => adminFetch<AdminSessionInfo>('/session'),
  login: (password: string) =>
    adminFetch<{ ok: boolean }>('/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  logout: () =>
    adminFetch<{ ok: boolean }>('/logout', {
      method: 'POST',
      body: '{}',
    }),
  config: () => adminFetch<AdminConfigSnapshot>('/config'),

  saveAuth: (body: { rootPassword?: string; adminPassword?: string }) =>
    adminFetch<{ ok: boolean; config: AdminConfigSnapshot }>('/config/auth', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  saveNetease: (body: { cookie?: string; clear?: boolean }) =>
    adminFetch<{ ok: boolean; config: AdminConfigSnapshot }>('/config/netease', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  saveBilibili: (body: {
    enabled?: boolean;
    useLogin?: boolean;
    cookie?: string;
    clearCookie?: boolean;
  }) =>
    adminFetch<{ ok: boolean; config: AdminConfigSnapshot }>('/config/bilibili', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  saveStorage: (body: {
    enabled: boolean;
    maxMBPerAccount: number;
    evictAfterDays: number;
    minHits: number;
    accounts: SaveStorageAccountBody[];
  }) =>
    adminFetch<{ ok: boolean; config: AdminConfigSnapshot }>('/config/storage', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  saveKeys: (body: { set?: Record<string, string>; delete?: string[] }) =>
    adminFetch<{ ok: boolean; config: AdminConfigSnapshot }>('/config/keys', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  neteaseStatus: () => adminFetch<NeteaseAccountStatus>('/netease/status'),
  neteaseQRCreate: () =>
    adminFetch<QRCreateResp>('/netease/qr/create', { method: 'POST', body: '{}' }),
  neteaseQRPoll: (key: string) =>
    adminFetch<QRPollResp>(`/netease/qr/poll?key=${encodeURIComponent(key)}`),

  biliStatus: () => adminFetch<BiliAccountStatus>('/bilibili/status'),
  biliQRCreate: () =>
    adminFetch<QRCreateResp>('/bilibili/qr/create', { method: 'POST', body: '{}' }),
  biliQRPoll: (key: string) =>
    adminFetch<QRPollResp>(`/bilibili/qr/poll?key=${encodeURIComponent(key)}`),
};
