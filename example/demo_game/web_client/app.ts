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
  type ConnectionState,
  type MatchFoundNotify,
  type GameStartNotify,
  type GuessResultNotify,
  type TurnNotify,
  type GameOverNotify,
  type TimeoutNotify,
  type ScoreUpdateNotify,
  type LeaderboardNotify,
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

  client.onConnect(() => {
    // Step 3: After connected, send JoinMatchRequest
    client!.send("JoinMatchRequest", { player_id: playerName });
  });

  // Step 4: Register message handlers via router
  client.router.on("MatchFoundNotify", (msg: MatchFoundNotify) => {
    console.log("Matched with:", msg.opponent_id);
  });

  client.router.on("GameStartNotify", (msg: GameStartNotify) => {
    state.inGame = true;
    state.myTurn = msg.your_turn;
    state.rangeLow = 1;
    state.rangeHigh = 100;
    state.round = 1;
    console.log("Game started!", msg.players.join(" vs "));
  });

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

  client.router.on("LeaderboardNotify", (msg: LeaderboardNotify) => {
    state.leaderboard = msg.entries;
  });

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
  client?.disconnect();
  client = null;
  state.inGame = false;
}
