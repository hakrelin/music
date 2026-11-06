/** HTTP 客户端：只封装 /api 的 JSON 接口，错误统一成 ApiError。 */

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(message: string, code = 'http_error', status = 0) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

async function parse(res: Response): Promise<unknown> {
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
    throw new ApiError(err?.message || `请求失败（HTTP ${res.status}）`, err?.code || 'http_error', res.status);
  }
  return data;
}

export async function apiGet<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, { headers: { Accept: 'application/json' }, ...init });
  return (await parse(res)) as T;
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body ?? {})
  });
  return (await parse(res)) as T;
}

export interface HealthInfo {
  ok: boolean;
  version: string;
  provider: string;
  rooms: number;
}

export interface CreateRoomBody {
  name: string;
  desc: string;
  needPwd: boolean;
  password?: string;
}

export const http = {
  health: () => apiGet<HealthInfo>('/healthz'),
  config: () => apiGet<import('@/types').SiteConfig>('/config'),
  rooms: () => apiGet<{ rooms: import('@/types').RoomSummary[] }>('/rooms'),
  createRoom: (body: CreateRoomBody) => apiPost<{ id: string }>('/rooms', body),
  authRoom: (id: string, password: string) =>
    apiPost<{ ok: boolean }>(`/rooms/${encodeURIComponent(id)}/auth`, { password })
};
