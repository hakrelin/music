import { apiGet } from '@/api/http';

export interface Sticker {
  id: string;
  name: string;
  mime: string;
  size: number;
  created: number;
  /** 编译进二进制的内置表情：不允许删除 */
  builtin?: boolean;
}

interface StickerListResponse {
  items: Sticker[];
}

interface StickerUploadResponse {
  ok: boolean;
  item: Sticker;
  url: string;
}

/** 表情包对外地址（服务端内容寻址，可长缓存）。 */
export function stickerUrl(id: string): string {
  return `/api/stickers/${id}`;
}

export async function listStickers(): Promise<Sticker[]> {
  const data = await apiGet<StickerListResponse>('/api/stickers');
  return data.items || [];
}

/**
 * 导入表情包：直接把 File 当请求体发（服务端嗅探真实类型并限制 2MB）。
 * 不用 FormData，省一层封装，也让进度/错误更直观。
 */
export async function uploadSticker(file: File): Promise<Sticker> {
  const resp = await fetch(`/api/stickers?name=${encodeURIComponent(file.name)}`, {
    method: 'POST',
    headers: { 'Content-Type': file.type || 'application/octet-stream' },
    body: file,
    credentials: 'same-origin',
  });
  if (!resp.ok) {
    const text = await resp.text().catch(() => '');
    let message = '导入失败';
    try {
      const parsed = JSON.parse(text) as { error?: { message?: string } };
      if (parsed.error?.message) message = parsed.error.message;
    } catch {
      /* 非 JSON 响应就退回通用文案 */
    }
    throw new Error(message);
  }
  const data = (await resp.json()) as StickerUploadResponse;
  return data.item;
}

export async function deleteSticker(id: string): Promise<void> {
  const resp = await fetch(`/api/stickers/${id}`, { method: 'DELETE', credentials: 'same-origin' });
  if (!resp.ok) {
    const text = await resp.text().catch(() => '');
    let message = '删除失败（需要管理员身份）';
    try {
      const parsed = JSON.parse(text) as { error?: { message?: string } };
      if (parsed.error?.message) message = parsed.error.message;
    } catch {
      /* 忽略 */
    }
    throw new Error(message);
  }
}
