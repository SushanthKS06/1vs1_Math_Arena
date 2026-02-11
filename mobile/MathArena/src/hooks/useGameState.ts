import { useState, useEffect, useCallback, useRef } from 'react';
import {
    gameSocket,
    GameSnapshot,
    MatchFoundPayload,
    QueueJoinedPayload,
    AnswerAckPayload,
    ErrorPayload
} from '../services/GameSocket';

export type UIState =
    | 'disconnected'
    | 'connecting'
    | 'idle'
    | 'in_queue'
    | 'match_found'
    | 'countdown'
    | 'answering'
    | 'waiting_for_round'
    | 'round_result'
    | 'game_over'
    | 'reconnecting';

export interface GameState {
    uiState: UIState;
    connected: boolean;
    queuePosition: number | null;
    snapshot: GameSnapshot | null;
    opponent: { playerId: string; displayName: string } | null;
    error: string | null;
    hasAnsweredThisRound: boolean;
}

const initialState: GameState = {
    uiState: 'disconnected',
    connected: false,
    queuePosition: null,
    snapshot: null,
    opponent: null,
    error: null,
    hasAnsweredThisRound: false,
};

export function useGameState(serverUrl: string) {
    const [state, setState] = useState<GameState>(initialState);
    const hasAnsweredRef = useRef(false);
    const currentRoundRef = useRef(0);

    const deriveUIState = useCallback((
        snapshot: GameSnapshot | null,
        connected: boolean,
        inQueue: boolean,
        hasAnswered: boolean
    ): UIState => {
        if (!connected) return 'disconnected';
        if (!snapshot) {
            if (inQueue) return 'in_queue';
            return 'idle';
        }

        switch (snapshot.state) {
            case 'waiting':
                return 'match_found';
            case 'countdown':
                return 'countdown';
            case 'round_active':
                return hasAnswered ? 'waiting_for_round' : 'answering';
            case 'round_end':
                return 'round_result';
            case 'game_over':
            case 'abandoned':
                return 'game_over';
            default:
                return 'idle';
        }
    }, []);

    const connect = useCallback(async (playerId: string, displayName: string) => {
        setState(s => ({ ...s, uiState: 'connecting', error: null }));

        try {
            await gameSocket.connect(serverUrl, playerId, displayName);
            setState(s => ({ ...s, connected: true, uiState: 'idle' }));
        } catch (error) {
            setState(s => ({
                ...s,
                connected: false,
                uiState: 'disconnected',
                error: 'Failed to connect to server'
            }));
        }
    }, [serverUrl]);

    useEffect(() => {
        const unsubscribers: (() => void)[] = [];

        unsubscribers.push(
            gameSocket.onConnectionChange((connected) => {
                setState(s => {
                    const uiState = connected
                        ? deriveUIState(s.snapshot, true, s.queuePosition !== null, hasAnsweredRef.current)
                        : 'disconnected';
                    return { ...s, connected, uiState };
                });
            })
        );

        unsubscribers.push(
            gameSocket.on<QueueJoinedPayload>('QUEUE_JOINED', (payload) => {
                setState(s => ({
                    ...s,
                    queuePosition: payload.position,
                    uiState: 'in_queue'
                }));
            })
        );

        unsubscribers.push(
            gameSocket.on('QUEUE_LEFT', () => {
                setState(s => ({
                    ...s,
                    queuePosition: null,
                    uiState: s.connected ? 'idle' : 'disconnected'
                }));
            })
        );

        unsubscribers.push(
            gameSocket.on<MatchFoundPayload>('MATCH_FOUND', (payload) => {
                setState(s => ({
                    ...s,
                    queuePosition: null,
                    opponent: {
                        playerId: payload.opponent.player_id,
                        displayName: payload.opponent.display_name,
                    },
                    uiState: 'match_found',
                }));
            })
        );

        unsubscribers.push(
            gameSocket.on<GameSnapshot>('GAME_SNAPSHOT', (snapshot) => {
                if (snapshot.round !== currentRoundRef.current) {
                    hasAnsweredRef.current = false;
                    currentRoundRef.current = snapshot.round;
                }

                const playerId = gameSocket.getPlayerId();
                const playerData = snapshot.players.find(p => p.player_id === playerId);
                if (playerData?.has_answered) {
                    hasAnsweredRef.current = true;
                }

                setState(s => ({
                    ...s,
                    snapshot,
                    hasAnsweredThisRound: hasAnsweredRef.current,
                    uiState: deriveUIState(snapshot, s.connected, false, hasAnsweredRef.current),
                    error: null,
                }));
            })
        );

        unsubscribers.push(
            gameSocket.on<AnswerAckPayload>('ANSWER_ACK', (payload) => {
                if (payload.accepted) {
                    hasAnsweredRef.current = true;
                    setState(s => ({
                        ...s,
                        hasAnsweredThisRound: true,
                        uiState: 'waiting_for_round',
                    }));
                } else {
                    setState(s => ({
                        ...s,
                        error: payload.reason || 'Answer rejected',
                    }));
                }
            })
        );

        unsubscribers.push(
            gameSocket.on<ErrorPayload>('ERROR', (payload) => {
                setState(s => ({ ...s, error: payload.message }));
            })
        );

        return () => {
            unsubscribers.forEach(unsub => unsub());
        };
    }, [deriveUIState]);

    const joinQueue = useCallback(() => {
        setState(s => ({ ...s, error: null }));
        gameSocket.joinQueue();
    }, []);

    const leaveQueue = useCallback(() => {
        gameSocket.leaveQueue();
        setState(s => ({ ...s, queuePosition: null, uiState: 'idle' }));
    }, []);

    const submitAnswer = useCallback((answer: number) => {
        if (!state.snapshot || hasAnsweredRef.current) return;

        gameSocket.submitAnswer(state.snapshot.round, answer);
    }, [state.snapshot]);

    const disconnect = useCallback(() => {
        gameSocket.disconnect();
        setState(initialState);
        hasAnsweredRef.current = false;
        currentRoundRef.current = 0;
    }, []);

    const clearError = useCallback(() => {
        setState(s => ({ ...s, error: null }));
    }, []);

    const getTimeRemaining = useCallback((): number => {
        if (!state.snapshot?.deadline_ts) return 0;

        const estimatedServerTime = gameSocket.getEstimatedServerTime();
        const remaining = state.snapshot.deadline_ts - estimatedServerTime;

        return Math.max(0, Math.floor(remaining / 1000));
    }, [state.snapshot]);

    return {
        ...state,
        connect,
        joinQueue,
        leaveQueue,
        submitAnswer,
        disconnect,
        clearError,
        getTimeRemaining,
    };
}
