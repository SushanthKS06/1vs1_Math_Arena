export type MessageType =
  | 'JOIN_QUEUE'
  | 'LEAVE_QUEUE'
  | 'ANSWER'
  | 'RECONNECT'
  | 'PING'
  | 'QUEUE_JOINED'
  | 'QUEUE_LEFT'
  | 'MATCH_FOUND'
  | 'GAME_SNAPSHOT'
  | 'ANSWER_ACK'
  | 'ERROR'
  | 'PONG';

export interface Message<T = unknown> {
  type: MessageType;
  payload: T;
  ts: number;
}

export interface PlayerInfo {
  player_id: string;
  display_name: string;
}

export interface PlayerData {
  player_id: string;
  display_name: string;
  score: number;
  connected: boolean;
  has_answered: boolean;
}

export interface QuestionData {
  id: number;
  expression: string;
}

export interface RoundResults {
  correct_answer: number;
  player_answers: {
    [playerId: string]: {
      answer: number;
      correct: boolean;
      points: number;
    };
  };
}

export interface GameSnapshot {
  game_id: string;
  state: 'waiting' | 'countdown' | 'round_active' | 'round_end' | 'game_over' | 'abandoned';
  round: number;
  total_rounds: number;
  question?: QuestionData;
  deadline_ts?: number;
  server_ts: number;
  players: PlayerData[];
  round_results?: RoundResults;
  winner?: string;
}

export interface MatchFoundPayload {
  game_id: string;
  opponent: PlayerInfo;
  starts_at: number;
}

export interface QueueJoinedPayload {
  position: number;
}

export interface AnswerAckPayload {
  round: number;
  accepted: boolean;
  reason?: string;
}

export interface ErrorPayload {
  code: string;
  message: string;
}

type MessageHandler<T = unknown> = (payload: T) => void;

class GameSocket {
  private ws: WebSocket | null = null;
  private serverUrl: string = '';
  private playerId: string = '';
  private displayName: string = '';
  private gameId: string | null = null;

  private reconnectAttempts: number = 0;
  private maxReconnectAttempts: number = 5;
  private reconnectDelay: number = 1000;
  private isReconnecting: boolean = false;

  private listeners: Map<string, Set<MessageHandler>> = new Map();
  private connectionListeners: Set<(connected: boolean) => void> = new Set();

  private pingInterval: ReturnType<typeof setInterval> | null = null;

  private serverTimeOffset: number = 0;

  async connect(serverUrl: string, playerId: string, displayName: string): Promise<void> {
    this.serverUrl = serverUrl;
    this.playerId = playerId;
    this.displayName = displayName;

    return this.doConnect();
  }

  private doConnect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const url = `${this.serverUrl}?player_id=${encodeURIComponent(this.playerId)}&display_name=${encodeURIComponent(this.displayName)}`;

      this.ws = new WebSocket(url);

      this.ws.onopen = () => {
        console.log('[GameSocket] Connected');
        this.reconnectAttempts = 0;
        this.isReconnecting = false;
        this.notifyConnectionListeners(true);
        this.startPing();

        if (this.gameId) {
          this.send('RECONNECT', {
            player_id: this.playerId,
            game_id: this.gameId,
          });
        }

        resolve();
      };

      this.ws.onmessage = (event) => {
        try {
          const msg: Message = JSON.parse(event.data);
          this.handleMessage(msg);
        } catch (e) {
          console.error('[GameSocket] Failed to parse message:', e);
        }
      };

      this.ws.onclose = (event) => {
        console.log('[GameSocket] Disconnected:', event.code, event.reason);
        this.stopPing();
        this.notifyConnectionListeners(false);

        if (!this.isReconnecting) {
          this.attemptReconnect();
        }
      };

      this.ws.onerror = (error) => {
        console.error('[GameSocket] Error:', error);
        reject(error);
      };
    });
  }

  private handleMessage(msg: Message): void {
    console.log('[GameSocket] Received:', msg.type);

    if (msg.type === 'MATCH_FOUND') {
      const payload = msg.payload as MatchFoundPayload;
      this.gameId = payload.game_id;
    }

    if (msg.type === 'GAME_SNAPSHOT') {
      const snapshot = msg.payload as GameSnapshot;

      this.serverTimeOffset = snapshot.server_ts - Date.now();

      if (snapshot.state === 'game_over' || snapshot.state === 'abandoned') {
        this.gameId = null;
      }
    }

    const handlers = this.listeners.get(msg.type);
    if (handlers) {
      handlers.forEach(handler => handler(msg.payload));
    }
  }

  private attemptReconnect(): void {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.log('[GameSocket] Max reconnect attempts reached');
      return;
    }

    this.isReconnecting = true;
    this.reconnectAttempts++;

    const delay = Math.min(
      this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1),
      10000
    );

    console.log(`[GameSocket] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);

    setTimeout(() => {
      this.doConnect().catch(() => {
      });
    }, delay);
  }

  private startPing(): void {
    this.pingInterval = setInterval(() => {
      this.send('PING', {});
    }, 30000);
  }

  private stopPing(): void {
    if (this.pingInterval) {
      clearInterval(this.pingInterval);
      this.pingInterval = null;
    }
  }

  send<T>(type: MessageType, payload: T): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      const msg: Message<T> = {
        type,
        payload,
        ts: Date.now(),
      };
      this.ws.send(JSON.stringify(msg));
      console.log('[GameSocket] Sent:', type);
    } else {
      console.warn('[GameSocket] Cannot send, not connected');
    }
  }

  on<T>(type: MessageType, handler: MessageHandler<T>): () => void {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, new Set());
    }
    this.listeners.get(type)!.add(handler as MessageHandler);

    return () => {
      this.listeners.get(type)?.delete(handler as MessageHandler);
    };
  }

  onConnectionChange(handler: (connected: boolean) => void): () => void {
    this.connectionListeners.add(handler);
    return () => {
      this.connectionListeners.delete(handler);
    };
  }

  private notifyConnectionListeners(connected: boolean): void {
    this.connectionListeners.forEach(handler => handler(connected));
  }

  joinQueue(): void {
    this.send('JOIN_QUEUE', {
      player_id: this.playerId,
      display_name: this.displayName,
    });
  }

  leaveQueue(): void {
    this.send('LEAVE_QUEUE', {});
  }

  submitAnswer(round: number, answer: number): void {
    if (!this.gameId) {
      console.error('[GameSocket] Cannot submit answer: no active game');
      return;
    }

    this.send('ANSWER', {
      game_id: this.gameId,
      round,
      answer,
      client_ts: Date.now(),
    });
  }

  disconnect(): void {
    this.stopPing();
    this.isReconnecting = false;
    this.reconnectAttempts = this.maxReconnectAttempts;
    this.ws?.close();
    this.ws = null;
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  getGameId(): string | null {
    return this.gameId;
  }

  getPlayerId(): string {
    return this.playerId;
  }

  getServerTimeOffset(): number {
    return this.serverTimeOffset;
  }

  getEstimatedServerTime(): number {
    return Date.now() + this.serverTimeOffset;
  }
}

export const gameSocket = new GameSocket();
