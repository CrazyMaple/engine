// TypeScript SDK for Engine Demo Game
// Based on generated Enhanced TypeScript SDK patterns
//
// 本文件同时演示 codegen/templates_proto_sdk.go 中的强类型增强：
//   - RPCClient: call<K>(type, req, opts?) => Promise<Response>（Promise + 超时 + __rpc_id 回传）
//   - PushSubscriber: on / once / onPush<T>（async iterator）
// 生成代码见 `engine gen -input=...go -ts-rpc=rpc_enhance.ts`

// ============================================================
// Message Interfaces — Demo Game Protocol
// ============================================================

/** C2S: Join match queue */
export interface JoinMatchRequest {
  player_id: string;
}

/** C2S: Submit a guess */
export interface GuessRequest {
  number: number;
}

/** C2S: Leave current room */
export interface LeaveRoomRequest {}

/** S2C: Match found, entering room */
export interface MatchFoundNotify {
  room_id: string;
  opponent_id: string;
}

/** S2C: Game started */
export interface GameStartNotify {
  your_turn: boolean;
  players: string[];
  target_hint: string;
}

/** S2C: Guess result */
export interface GuessResultNotify {
  player_id: string;
  number: number;
  result: "too_low" | "too_high" | "correct";
}

/** S2C: Your turn to guess */
export interface TurnNotify {
  round: number;
}

/** S2C: Game over */
export interface GameOverNotify {
  winner_id: string;
  target_num: number;
  score: number;
  total_guess: number;
}

/** S2C: Timeout */
export interface TimeoutNotify {
  player_id: string;
  round: number;
}

/** S2C: Score update */
export interface ScoreUpdateNotify {
  player_id: string;
  score: number;
  rank: number;
}

/** S2C: Leaderboard snapshot */
export interface LeaderboardNotify {
  entries: Array<{ rank: number; player_id: string; score: number }>;
}

// ============================================================
// Message Map — type-safe dispatch
// ============================================================

export interface MessageMap {
  JoinMatchRequest: JoinMatchRequest;
  GuessRequest: GuessRequest;
  LeaveRoomRequest: LeaveRoomRequest;
  MatchFoundNotify: MatchFoundNotify;
  GameStartNotify: GameStartNotify;
  GuessResultNotify: GuessResultNotify;
  TurnNotify: TurnNotify;
  GameOverNotify: GameOverNotify;
  TimeoutNotify: TimeoutNotify;
  ScoreUpdateNotify: ScoreUpdateNotify;
  LeaderboardNotify: LeaderboardNotify;
}

// ============================================================
// Codec — JSON encode/decode
// ============================================================

export interface Codec {
  encode(type: string, msg: any): string | ArrayBuffer;
  decode(data: string | ArrayBuffer): { type: string; payload: any } | null;
}

export class JSONCodec implements Codec {
  encode(type: string, msg: any): string {
    return JSON.stringify({ type, ...msg });
  }

  decode(data: string | ArrayBuffer): { type: string; payload: any } | null {
    try {
      const text = typeof data === "string" ? data : new TextDecoder().decode(data);
      const obj = JSON.parse(text);
      const type = obj.type;
      if (!type) return null;
      const payload = { ...obj };
      delete payload.type;
      return { type, payload };
    } catch {
      return null;
    }
  }
}

// ============================================================
// Message Router — type-safe handler dispatch
// ============================================================

type MessageHandler<T> = (msg: T) => void;

export class MessageRouter {
  private handlers = new Map<string, Set<Function>>();
  private wildcardHandlers = new Set<(type: string, msg: any) => void>();

  on<K extends keyof MessageMap>(type: K, handler: MessageHandler<MessageMap[K]>): () => void {
    if (!this.handlers.has(type as string)) {
      this.handlers.set(type as string, new Set());
    }
    this.handlers.get(type as string)!.add(handler);
    return () => this.handlers.get(type as string)?.delete(handler);
  }

  onAny(handler: (type: string, msg: any) => void): () => void {
    this.wildcardHandlers.add(handler);
    return () => this.wildcardHandlers.delete(handler);
  }

  dispatch(type: string, msg: any): boolean {
    let handled = false;
    const handlers = this.handlers.get(type);
    if (handlers && handlers.size > 0) {
      handlers.forEach((h) => h(msg));
      handled = true;
    }
    if (this.wildcardHandlers.size > 0) {
      this.wildcardHandlers.forEach((h) => h(type, msg));
      handled = true;
    }
    return handled;
  }

  clear(): void {
    this.handlers.clear();
    this.wildcardHandlers.clear();
  }
}

// ============================================================
// GameClient — WebSocket connection + auto-reconnect + heartbeat
// ============================================================

export type ConnectionState = "disconnected" | "connecting" | "connected" | "reconnecting";

export interface ClientOptions {
  url: string;
  reconnect?: boolean;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
  heartbeatInterval?: number;
}

export class GameClient {
  private ws: WebSocket | null = null;
  private codec = new JSONCodec();
  private opts: Required<ClientOptions>;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private closed = false;
  private _state: ConnectionState = "disconnected";
  private stateListeners = new Set<(s: ConnectionState) => void>();
  private connectListeners = new Set<() => void>();
  private disconnectListeners = new Set<(code: number, reason: string) => void>();
  private errorListeners = new Set<(err: Event | Error) => void>();

  public readonly router = new MessageRouter();

  constructor(options: ClientOptions) {
    this.opts = {
      url: options.url,
      reconnect: options.reconnect ?? true,
      reconnectInterval: options.reconnectInterval ?? 3000,
      maxReconnectAttempts: options.maxReconnectAttempts ?? 10,
      heartbeatInterval: options.heartbeatInterval ?? 30000,
    };
  }

  get state(): ConnectionState { return this._state; }
  get connected(): boolean { return this._state === "connected"; }

  connect(): Promise<void> {
    this.closed = false;
    this.setState("connecting");
    return new Promise((resolve, reject) => {
      try {
        this.ws = new WebSocket(this.opts.url);
      } catch (err) {
        this.setState("disconnected");
        reject(err);
        return;
      }

      this.ws.onopen = () => {
        this.reconnectAttempts = 0;
        this.startHeartbeat();
        // Handshake
        this.ws!.send(JSON.stringify({
          type: "__handshake__",
          protocol_version: 1,
          client_sdk: "ts-demo-1.0.0",
        }));
      };

      let handshakeDone = false;
      this.ws.onmessage = (event: MessageEvent) => {
        if (!handshakeDone && typeof event.data === "string" && event.data.includes("__handshake_ack__")) {
          handshakeDone = true;
          this.setState("connected");
          this.connectListeners.forEach((h) => h());
          resolve();
          return;
        }
        const decoded = this.codec.decode(event.data);
        if (decoded) {
          this.router.dispatch(decoded.type, decoded.payload);
        }
      };

      this.ws.onerror = (event: Event) => {
        this.errorListeners.forEach((h) => h(event));
        if (!handshakeDone) reject(event);
      };

      this.ws.onclose = (event: CloseEvent) => {
        this.stopHeartbeat();
        this.disconnectListeners.forEach((h) => h(event.code, event.reason));
        if (!this.closed && this.opts.reconnect) {
          this.tryReconnect();
        } else {
          this.setState("disconnected");
        }
      };
    });
  }

  disconnect(): void {
    this.closed = true;
    this.stopHeartbeat();
    if (this.reconnectTimer) { clearTimeout(this.reconnectTimer); this.reconnectTimer = null; }
    if (this.ws) { this.ws.close(1000, "client disconnect"); this.ws = null; }
    this.setState("disconnected");
  }

  send<K extends keyof MessageMap>(type: K, msg: MessageMap[K]): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(this.codec.encode(type as string, msg));
  }

  onConnect(handler: () => void): () => void {
    this.connectListeners.add(handler);
    return () => this.connectListeners.delete(handler);
  }

  onDisconnect(handler: (code: number, reason: string) => void): () => void {
    this.disconnectListeners.add(handler);
    return () => this.disconnectListeners.delete(handler);
  }

  onError(handler: (err: Event | Error) => void): () => void {
    this.errorListeners.add(handler);
    return () => this.errorListeners.delete(handler);
  }

  onStateChange(handler: (s: ConnectionState) => void): () => void {
    this.stateListeners.add(handler);
    return () => this.stateListeners.delete(handler);
  }

  private setState(state: ConnectionState): void {
    if (this._state === state) return;
    this._state = state;
    this.stateListeners.forEach((h) => h(state));
  }

  private tryReconnect(): void {
    if (this.opts.maxReconnectAttempts > 0 && this.reconnectAttempts >= this.opts.maxReconnectAttempts) {
      this.setState("disconnected");
      return;
    }
    this.reconnectAttempts++;
    this.setState("reconnecting");
    this.reconnectTimer = setTimeout(() => { this.connect().catch(() => {}); }, this.opts.reconnectInterval);
  }

  private startHeartbeat(): void {
    if (this.opts.heartbeatInterval <= 0) return;
    this.heartbeatTimer = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ type: "__ping__" }));
      }
    }, this.opts.heartbeatInterval);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) { clearInterval(this.heartbeatTimer); this.heartbeatTimer = null; }
  }
}

// ============================================================
// RPC / Push 强类型增强层（对应 codegen/templates_proto_sdk.go 的 tsRPCEnhancedTemplate）
// 这里为手写 SDK 的等价实现，演示 RPC 风格 API、Promise+超时、OnPush<T> 三件套
// ============================================================

/** RPC Request → Response 名字映射。Demo 协议无 XxxResponse 命名对，故此表为空 */
export interface RPCMap {
  // 示例（当你有 LoginRequest/LoginResponse 时自动生成）：
  // LoginRequest: "LoginResponse";
}

export type RPCRequestType = keyof RPCMap;
export type RPCResponseType<K extends RPCRequestType> = RPCMap[K];

export interface RPCCallOptions {
  /** 超时毫秒数，0 = 不超时（默认 5000） */
  timeoutMs?: number;
  /** AbortSignal */
  signal?: AbortSignal;
}

export class RPCTimeoutError extends Error {
  readonly name = "RPCTimeoutError";
  constructor(public readonly requestType: string, public readonly timeoutMs: number) {
    super(`RPC timeout: ${requestType} after ${timeoutMs}ms`);
  }
}

export class RPCAbortError extends Error {
  readonly name = "RPCAbortError";
  constructor(public readonly requestType: string) { super(`RPC aborted: ${requestType}`); }
}

interface PendingCall {
  resolve: (msg: any) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout> | null;
}

/**
 * RPCClient — 基于 MessageMap 的强类型 RPC 封装
 *
 * 请求附带 __rpc_id，服务端需原样回传至响应；未实现时退化为匹配最早未决请求
 */
export class RPCClient {
  private seq = 0;
  private pending = new Map<number, PendingCall>();
  private unsubs: Array<() => void> = [];
  private installed = new Set<string>();
  private rpcPairs: Record<string, string>;

  constructor(private client: GameClient, rpcPairs: Record<string, string> = {}) {
    this.rpcPairs = rpcPairs;
  }

  call<K extends RPCRequestType>(
    type: K, req: MessageMap[K], opts: RPCCallOptions = {}
  ): Promise<MessageMap[RPCResponseType<K>]>;
  call(type: string, req: any, opts?: RPCCallOptions): Promise<any>;
  call(type: string, req: any, opts: RPCCallOptions = {}): Promise<any> {
    const respType = this.rpcPairs[type];
    if (!respType) return Promise.reject(new Error("not an RPC request: " + type));
    this.ensureSubscribed(respType);

    return new Promise((resolve, reject) => {
      const id = ++this.seq;
      const timeoutMs = opts.timeoutMs ?? 5000;
      const entry: PendingCall = { resolve, reject, timer: null };
      const cleanup = () => {
        if (entry.timer) clearTimeout(entry.timer);
        this.pending.delete(id);
      };
      entry.resolve = (m) => { cleanup(); resolve(m); };
      entry.reject = (e) => { cleanup(); reject(e); };

      if (timeoutMs > 0) {
        entry.timer = setTimeout(() => entry.reject(new RPCTimeoutError(type, timeoutMs)), timeoutMs);
      }
      if (opts.signal) {
        if (opts.signal.aborted) { entry.reject(new RPCAbortError(type)); return; }
        opts.signal.addEventListener("abort", () => entry.reject(new RPCAbortError(type)), { once: true });
      }
      this.pending.set(id, entry);
      try { this.client.send(type as any, { ...(req as object), __rpc_id: id } as any); }
      catch (err) { entry.reject(err instanceof Error ? err : new Error(String(err))); }
    });
  }

  dispose(): void {
    this.pending.forEach(p => p.reject(new Error("RPCClient disposed")));
    this.pending.clear();
    this.unsubs.forEach(fn => fn());
    this.unsubs.length = 0;
    this.installed.clear();
  }

  private ensureSubscribed(respType: string): void {
    if (this.installed.has(respType)) return;
    this.installed.add(respType);
    const off = this.client.router.onAny((t, msg) => {
      if (t !== respType) return;
      const rpcId = (msg as { __rpc_id?: number })?.__rpc_id;
      if (typeof rpcId === "number") {
        const p = this.pending.get(rpcId);
        if (p) { p.resolve(msg); return; }
      }
      for (const [id, p] of this.pending) { this.pending.delete(id); p.resolve(msg); return; }
    });
    this.unsubs.push(off);
  }
}

export interface PushOptions {
  signal?: AbortSignal;
  limit?: number;
}

export interface PushSubscription<T> {
  unsubscribe(): void;
  [Symbol.asyncIterator](): AsyncIterator<T>;
  next(): Promise<T>;
}

/**
 * PushSubscriber — 基于 MessageMap 的强类型事件订阅器
 * - on / once / onPush<T> 三种消费方式
 * - AbortSignal 友好，limit 限流
 */
export class PushSubscriber {
  constructor(private client: GameClient) {}

  on<K extends keyof MessageMap>(type: K, handler: (msg: MessageMap[K]) => void): { unsubscribe(): void } {
    const off = this.client.router.on(type, handler);
    return { unsubscribe: off };
  }

  once<K extends keyof MessageMap>(type: K, opts: PushOptions = {}): Promise<MessageMap[K]> {
    return new Promise((resolve, reject) => {
      let settled = false;
      const off = this.client.router.on(type, (msg) => {
        if (settled) return;
        settled = true; off(); resolve(msg as MessageMap[K]);
      });
      if (opts.signal) {
        if (opts.signal.aborted) { settled = true; off(); reject(new RPCAbortError(type as string)); return; }
        opts.signal.addEventListener("abort", () => {
          if (settled) return;
          settled = true; off(); reject(new RPCAbortError(type as string));
        }, { once: true });
      }
    });
  }

  onPush<K extends keyof MessageMap>(type: K, opts: PushOptions = {}): PushSubscription<MessageMap[K]> {
    const self = this;
    const buffer: MessageMap[K][] = [];
    const waiters: Array<(v: IteratorResult<MessageMap[K]>) => void> = [];
    let received = 0;
    let done = false;

    const finish = () => {
      if (done) return;
      done = true; off();
      while (waiters.length) {
        waiters.shift()!({ value: undefined as unknown as MessageMap[K], done: true });
      }
    };
    const off = this.client.router.on(type, (msg) => {
      if (done) return;
      if (waiters.length) waiters.shift()!({ value: msg as MessageMap[K], done: false });
      else buffer.push(msg as MessageMap[K]);
      received++;
      if (opts.limit && received >= opts.limit) finish();
    });
    if (opts.signal) {
      if (opts.signal.aborted) finish();
      else opts.signal.addEventListener("abort", finish, { once: true });
    }
    return {
      unsubscribe: finish,
      [Symbol.asyncIterator](): AsyncIterator<MessageMap[K]> {
        return {
          next: () => {
            if (buffer.length) return Promise.resolve({ value: buffer.shift()!, done: false });
            if (done) return Promise.resolve({ value: undefined as unknown as MessageMap[K], done: true });
            return new Promise<IteratorResult<MessageMap[K]>>(r => waiters.push(r));
          },
          return: () => {
            finish();
            return Promise.resolve({ value: undefined as unknown as MessageMap[K], done: true });
          },
        };
      },
      next: () => self.once(type, opts),
    };
  }
}

export function createRPC(client: GameClient, pairs: Record<string, string> = {}): RPCClient {
  return new RPCClient(client, pairs);
}

export function createPush(client: GameClient): PushSubscriber {
  return new PushSubscriber(client);
}
