// Demo Game Web Client — Application Logic
// Best-practice example of using the Engine TypeScript SDK
//
// Usage:
//   1. Start the game server: go run example/demo_game/main.go
//   2. Open web_client/index.html in a browser
//   3. Enter your name, connect, and play!
//
// This file demonstrates:
//   - GameClient instantiation with WebSocket URL
//   - Message router for type-safe handler registration
//   - Connection lifecycle (connect/disconnect/reconnect)
//   - Sending C2S messages and handling S2C messages

import {
  GameClient,
  PushSubscriber,
  type ConnectionState,
  type GuessResultNotify,
  type TurnNotify,
  type GameOverNotify,
  type TimeoutNotify,
  type ScoreUpdateNotify,
} from "./sdk";

// ============================================================
// Game State
// ============================================================

interface AppState {
  playerName: string;
  inGame: boolean;
  myTurn: boolean;
  rangeLow: number;
  rangeHigh: number;
  round: number;
  leaderboard: Array<{ rank: number; player_id: string; score: number }>;
}

const state: AppState = {
  playerName: "",
  inGame: false,
  myTurn: false,
  rangeLow: 1,
  rangeHigh: 100,
  round: 0,
  leaderboard: [],
};

let client: GameClient | null = null;
let push: PushSubscriber | null = null;
let leaderboardAbort: AbortController | null = null;

// ============================================================
// SDK Usage Example — Connect & Register Handlers
// ============================================================

export function connectToServer(url: string, playerName: string): void {
  state.playerName = playerName;

  // Step 1: Create client with options
  client = new GameClient({
    url,
    reconnect: true,
    maxReconnectAttempts: 5,
    heartbeatInterval: 30000,
  });

  // Step 2: Listen for connection state changes
  client.onStateChange((s: ConnectionState) => {
    console.log("[state]", s);
  });

  // Step 3: 基于 PushSubscriber 的强类型订阅
  push = new PushSubscriber(client);

  client.onConnect(async () => {
    // Step 4: After connected, send JoinMatchRequest
    client!.send("JoinMatchRequest", { player_id: playerName });

    // Step 4a: 用 push.once 等待 MatchFoundNotify（带超时）— 替代手写 on + 一次性清理
    try {
      const matched = await push!.once("MatchFoundNotify", {
        signal: AbortSignal.timeout(10_000),
      });
      console.log("Matched with:", matched.opponent_id);
    } catch (err) {
      console.warn("match wait failed:", err);
      return;
    }

    // Step 4b: 用 push.once 等待 GameStartNotify 作为游戏初始化信号
    const start = await push!.once("GameStartNotify");
    state.inGame = true;
    state.myTurn = start.your_turn;
    state.rangeLow = 1;
    state.rangeHigh = 100;
    state.round = 1;
    console.log("Game started!", start.players.join(" vs "));

    // Step 4c: 用 onPush<T> 异步迭代消费排行榜流（AbortController 控制生命周期）
    leaderboardAbort?.abort();
    leaderboardAbort = new AbortController();
    (async () => {
      const stream = push!.onPush("LeaderboardNotify", { signal: leaderboardAbort!.signal });
      for await (const msg of stream) {
        state.leaderboard = msg.entries;
      }
    })().catch(console.warn);
  });

  // 回合/命中/结算等高频事件仍用回调风格订阅
  client.router.on("GuessResultNotify", (msg: GuessResultNotify) => {
    if (msg.result === "too_low" && msg.number >= state.rangeLow) {
      state.rangeLow = msg.number + 1;
    } else if (msg.result === "too_high" && msg.number <= state.rangeHigh) {
      state.rangeHigh = msg.number - 1;
    }
    console.log(`${msg.player_id} guessed ${msg.number} -> ${msg.result}`);
  });

  client.router.on("TurnNotify", (msg: TurnNotify) => {
    state.myTurn = true;
    state.round = msg.round;
  });

  client.router.on("GameOverNotify", (msg: GameOverNotify) => {
    state.inGame = false;
    state.myTurn = false;
    console.log(`Game Over! Winner: ${msg.winner_id}, Score: ${msg.score}`);
  });

  client.router.on("TimeoutNotify", (msg: TimeoutNotify) => {
    if (msg.player_id === state.playerName) state.myTurn = false;
  });

  client.router.on("ScoreUpdateNotify", (_msg: ScoreUpdateNotify) => {});

  // LeaderboardNotify 已在 onConnect 中通过 push.onPush(..., { signal }) 的 async iterator 消费

  // Step 5: Connect (returns a Promise)
  client.connect().catch(console.error);
}

// Step 6: Send a guess
export function sendGuess(number: number): void {
  if (!client?.connected || !state.myTurn) return;
  state.myTurn = false;
  client.send("GuessRequest", { number });
}

// Step 7: Disconnect
export function disconnect(): void {
  leaderboardAbort?.abort();
  leaderboardAbort = null;
  push = null;
  client?.disconnect();
  client = null;
  state.inGame = false;
}
