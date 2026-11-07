import type { ServerMsg } from '@/types';

interface Pending {
  resolve: (m: ServerMsg) => void;
  reject: (e: Error) => void;
  timer: number;
}

interface Options {
  url: string;
  onOpen?: () => void;
  onStatus?: (connected: boolean) => void;
  onEvent: (m: ServerMsg) => void;
}

/**
 * 房间 WebSocket：自动重连 + 心跳 + 请求-响应配对。
 * 事件（无 req）交给 onEvent；带 req 的响应解析对应 Promise。
 */
export class RoomSocket {
  private ws: WebSocket | null = null;
  private pending = new Map<string, Pending>();
  private seq = 0;
  private heartbeat = 0;
  private reconnectTimer = 0;
  private probeTimer = 0;
  private resumePage = () => {
    if (!document.hidden) this.resume();
  };
  private attempt = 0;
  private closedByUser = false;
  /** 估算的服务器时钟偏移：serverNow - Date.now() */
  offset = 0;

  constructor(private opts: Options) {}

  connect(): void {
    this.closedByUser = false;
    document.addEventListener('visibilitychange', this.resumePage);
    window.addEventListener('pageshow', this.resumePage);
    this.open();
  }

  /** Background suspension can leave an apparently OPEN, but dead, connection. */
  resume(): void {
    if (this.closedByUser) return;
    window.clearTimeout(this.reconnectTimer);
    window.clearTimeout(this.probeTimer);
    const ws = this.ws;
    if (!ws || ws.readyState === WebSocket.CLOSED) {
      this.open();
      return;
    }
    if (ws.readyState !== WebSocket.OPEN) return;
    this.send({ t: 'ping' });
    this.probeTimer = window.setTimeout(() => {
      if (this.ws !== ws || this.closedByUser) return;
      this.stopHeartbeat();
      this.failPending(new Error('连接正在恢复'));
      this.opts.onStatus?.(false);
      ws.close();
      this.open();
    }, 3000);
  }

  private open(): void {
    const ws = new WebSocket(this.opts.url);
    this.ws = ws;
    ws.onopen = () => {
      if (this.ws !== ws || this.closedByUser) return;
      this.attempt = 0;
      this.opts.onStatus?.(true);
      this.opts.onOpen?.();
      this.startHeartbeat();
    };
    ws.onmessage = (ev) => {
      if (this.ws === ws && !this.closedByUser) this.handle(ev);
    };
    ws.onclose = () => {
      if (this.ws !== ws || this.closedByUser) return;
      window.clearTimeout(this.probeTimer);
      this.stopHeartbeat();
      this.opts.onStatus?.(false);
      this.failPending(new Error('连接已断开'));
      if (!this.closedByUser) this.scheduleReconnect();
    };
    ws.onerror = () => {
      /* onclose 会处理 */
    };
  }

  private scheduleReconnect(): void {
    window.clearTimeout(this.reconnectTimer);
    const delay = Math.min(8000, 500 * 2 ** this.attempt);
    this.attempt += 1;
    this.reconnectTimer = window.setTimeout(() => {
      if (!this.closedByUser) this.open();
    }, delay);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    const ping = () => {
      if (this.ws?.readyState !== WebSocket.OPEN) return;
      this.send({ t: 'ping' });
    };
    ping();
    this.heartbeat = window.setInterval(ping, 20000);
  }

  private stopHeartbeat(): void {
    if (this.heartbeat) window.clearInterval(this.heartbeat);
    this.heartbeat = 0;
  }

  private handle(ev: MessageEvent): void {
    let msg: ServerMsg;
    try {
      msg = JSON.parse(String(ev.data)) as ServerMsg;
    } catch {
      return;
    }
    window.clearTimeout(this.probeTimer);
    if (msg.t === 'pong' && typeof msg.now === 'number') {
      this.offset = msg.now - Date.now();
      return;
    }
    const req = typeof msg.req === 'string' ? msg.req : '';
    if (req && this.pending.has(req)) {
      const p = this.pending.get(req)!;
      this.pending.delete(req);
      window.clearTimeout(p.timer);
      if (msg.t === 'error') {
        p.reject(new Error(String(msg.message || '请求失败')));
      } else {
        p.resolve(msg);
      }
      return;
    }
    this.opts.onEvent(msg);
  }

  private failPending(err: Error): void {
    for (const [, p] of this.pending) {
      window.clearTimeout(p.timer);
      p.reject(err);
    }
    this.pending.clear();
  }

  send(msg: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  /** 发一个带 req 的请求并等对应响应。 */
  request<T extends ServerMsg = ServerMsg>(
    payload: Record<string, unknown>,
    timeoutMs = 12000,
  ): Promise<T> {
    this.seq += 1;
    const req = `r${this.seq}`;
    return new Promise<T>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending.delete(req);
        reject(new Error('请求超时'));
      }, timeoutMs);
      this.pending.set(req, { resolve: resolve as (m: ServerMsg) => void, reject, timer });
      if (this.ws?.readyState !== WebSocket.OPEN) {
        window.clearTimeout(timer);
        this.pending.delete(req);
        reject(new Error('尚未连接'));
        return;
      }
      this.send({ ...payload, req });
    });
  }

  close(): void {
    this.closedByUser = true;
    document.removeEventListener('visibilitychange', this.resumePage);
    window.removeEventListener('pageshow', this.resumePage);
    window.clearTimeout(this.probeTimer);
    window.clearTimeout(this.reconnectTimer);
    this.stopHeartbeat();
    this.failPending(new Error('已离开房间'));
    this.ws?.close();
    this.ws = null;
  }
}
