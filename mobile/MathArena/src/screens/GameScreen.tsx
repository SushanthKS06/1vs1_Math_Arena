import React, { useState, useEffect, useCallback } from 'react';
import {
    View,
    Text,
    TextInput,
    TouchableOpacity,
    StyleSheet,
    SafeAreaView,
    ActivityIndicator,
    Animated,
} from 'react-native';
import { useGameState, UIState } from '../hooks/useGameState';
import { GameSnapshot } from '../services/GameSocket';

import { Platform } from 'react-native';
const SERVER_URL = Platform.OS === 'web'
    ? 'ws://localhost:8888/ws'
    : 'ws://192.168.29.2:8888/ws';

const generatePlayerId = () => `player_${Math.random().toString(36).substring(2, 10)}`;

export default function GameScreen() {
    const [playerId] = useState(generatePlayerId);
    const [displayName, setDisplayName] = useState('');
    const [answerInput, setAnswerInput] = useState('');
    const [timeRemaining, setTimeRemaining] = useState(0);

    const game = useGameState(SERVER_URL);

    useEffect(() => {
        if (game.uiState !== 'answering' && game.uiState !== 'waiting_for_round') {
            return;
        }

        const interval = setInterval(() => {
            setTimeRemaining(game.getTimeRemaining());
        }, 100);

        return () => clearInterval(interval);
    }, [game.uiState, game.getTimeRemaining]);

    const handleConnect = useCallback(() => {
        if (!displayName.trim()) return;
        game.connect(playerId, displayName);
    }, [playerId, displayName, game]);

    const handleSubmit = useCallback(() => {
        const answer = parseInt(answerInput, 10);
        if (isNaN(answer)) return;

        game.submitAnswer(answer);
        setAnswerInput('');
    }, [answerInput, game]);

    const renderContent = () => {
        switch (game.uiState) {
            case 'disconnected':
                return <DisconnectedView name={displayName} setName={setDisplayName} onConnect={handleConnect} />;

            case 'connecting':
                return <LoadingView message="Connecting..." />;

            case 'idle':
                return <IdleView onJoinQueue={game.joinQueue} />;

            case 'in_queue':
                return <QueueView position={game.queuePosition} onLeave={game.leaveQueue} />;

            case 'match_found':
                return <MatchFoundView opponent={game.opponent?.displayName || 'Opponent'} />;

            case 'countdown':
                return <CountdownView />;

            case 'answering':
            case 'waiting_for_round':
                return (
                    <GameView
                        snapshot={game.snapshot!}
                        timeRemaining={timeRemaining}
                        answerInput={answerInput}
                        setAnswerInput={setAnswerInput}
                        onSubmit={handleSubmit}
                        hasAnswered={game.hasAnsweredThisRound}
                        playerId={playerId}
                    />
                );

            case 'round_result':
                return <RoundResultView snapshot={game.snapshot!} playerId={playerId} />;

            case 'game_over':
                return (
                    <GameOverView
                        snapshot={game.snapshot!}
                        playerId={playerId}
                        onPlayAgain={game.joinQueue}
                    />
                );

            default:
                return <LoadingView message="Loading..." />;
        }
    };

    return (
        <SafeAreaView style={styles.container}>
            <Header connected={game.connected} />
            {game.error && <ErrorBanner message={game.error} onDismiss={game.clearError} />}
            {renderContent()}
        </SafeAreaView>
    );
}

function Header({ connected }: { connected: boolean }) {
    return (
        <View style={styles.header}>
            <Text style={styles.title}>Math Arena</Text>
            <View style={[styles.statusDot, connected ? styles.connected : styles.disconnected]} />
        </View>
    );
}

function ErrorBanner({ message, onDismiss }: { message: string; onDismiss: () => void }) {
    return (
        <TouchableOpacity style={styles.errorBanner} onPress={onDismiss}>
            <Text style={styles.errorText}>{message}</Text>
        </TouchableOpacity>
    );
}

function LoadingView({ message }: { message: string }) {
    return (
        <View style={styles.centered}>
            <ActivityIndicator size="large" color="#6c5ce7" />
            <Text style={styles.loadingText}>{message}</Text>
        </View>
    );
}

function DisconnectedView({
    name,
    setName,
    onConnect
}: {
    name: string;
    setName: (n: string) => void;
    onConnect: () => void;
}) {
    return (
        <View style={styles.centered}>
            <Text style={styles.subtitle}>Enter your name to play</Text>
            <TextInput
                style={styles.input}
                placeholder="Your name"
                placeholderTextColor="#666"
                value={name}
                onChangeText={setName}
                autoCapitalize="words"
            />
            <TouchableOpacity
                style={[styles.button, !name.trim() && styles.buttonDisabled]}
                onPress={onConnect}
                disabled={!name.trim()}
            >
                <Text style={styles.buttonText}>Connect</Text>
            </TouchableOpacity>
        </View>
    );
}

function IdleView({ onJoinQueue }: { onJoinQueue: () => void }) {
    return (
        <View style={styles.centered}>
            <Text style={styles.subtitle}>Ready to play?</Text>
            <TouchableOpacity style={styles.button} onPress={onJoinQueue}>
                <Text style={styles.buttonText}>Find Match</Text>
            </TouchableOpacity>
        </View>
    );
}

function QueueView({ position, onLeave }: { position: number | null; onLeave: () => void }) {
    return (
        <View style={styles.centered}>
            <ActivityIndicator size="large" color="#6c5ce7" />
            <Text style={styles.subtitle}>Finding opponent...</Text>
            {position && <Text style={styles.queuePosition}>Position: {position}</Text>}
            <TouchableOpacity style={styles.buttonSecondary} onPress={onLeave}>
                <Text style={styles.buttonSecondaryText}>Cancel</Text>
            </TouchableOpacity>
        </View>
    );
}

function MatchFoundView({ opponent }: { opponent: string }) {
    return (
        <View style={styles.centered}>
            <Text style={styles.matchTitle}>Match Found!</Text>
            <Text style={styles.opponentName}>vs {opponent}</Text>
        </View>
    );
}

function CountdownView() {
    return (
        <View style={styles.centered}>
            <Text style={styles.countdownText}>Get Ready!</Text>
        </View>
    );
}

function GameView({
    snapshot,
    timeRemaining,
    answerInput,
    setAnswerInput,
    onSubmit,
    hasAnswered,
    playerId,
}: {
    snapshot: GameSnapshot;
    timeRemaining: number;
    answerInput: string;
    setAnswerInput: (v: string) => void;
    onSubmit: () => void;
    hasAnswered: boolean;
    playerId: string;
}) {
    const me = snapshot.players.find(p => p.player_id === playerId);
    const opponent = snapshot.players.find(p => p.player_id !== playerId);

    return (
        <View style={styles.gameContainer}>
            <View style={styles.scoreboard}>
                <PlayerScore name="You" score={me?.score || 0} highlight />
                <Text style={styles.vs}>vs</Text>
                <PlayerScore name={opponent?.display_name || 'Opponent'} score={opponent?.score || 0} />
            </View>

            <View style={styles.roundInfo}>
                <Text style={styles.roundText}>Round {snapshot.round} / {snapshot.total_rounds}</Text>
                <Text style={[styles.timer, timeRemaining <= 3 && styles.timerWarning]}>
                    {timeRemaining}s
                </Text>
            </View>

            <View style={styles.questionContainer}>
                <Text style={styles.questionText}>{snapshot.question?.expression || '...'}</Text>
            </View>

            {hasAnswered ? (
                <View style={styles.waitingContainer}>
                    <Text style={styles.waitingText}>Answer submitted! Waiting...</Text>
                </View>
            ) : (
                <View style={styles.answerContainer}>
                    <TextInput
                        style={styles.answerInput}
                        placeholder="Your answer"
                        placeholderTextColor="#666"
                        value={answerInput}
                        onChangeText={setAnswerInput}
                        keyboardType="number-pad"
                        autoFocus
                    />
                    <TouchableOpacity style={styles.submitButton} onPress={onSubmit}>
                        <Text style={styles.submitButtonText}>Submit</Text>
                    </TouchableOpacity>
                </View>
            )}
        </View>
    );
}

function PlayerScore({ name, score, highlight }: { name: string; score: number; highlight?: boolean }) {
    return (
        <View style={styles.playerScore}>
            <Text style={[styles.playerName, highlight && styles.playerNameHighlight]}>{name}</Text>
            <Text style={[styles.score, highlight && styles.scoreHighlight]}>{score}</Text>
        </View>
    );
}

function RoundResultView({ snapshot, playerId }: { snapshot: GameSnapshot; playerId: string }) {
    const results = snapshot.round_results;
    if (!results) return null;

    const myResult = results.player_answers[playerId];

    return (
        <View style={styles.centered}>
            <Text style={styles.resultTitle}>Round {snapshot.round} Results</Text>
            <Text style={styles.correctAnswer}>Correct: {results.correct_answer}</Text>
            {myResult && (
                <Text style={myResult.correct ? styles.resultCorrect : styles.resultWrong}>
                    Your answer: {myResult.answer} {myResult.correct ? '✓' : '✗'}
                </Text>
            )}
            <Text style={styles.nextRound}>Next round starting...</Text>
        </View>
    );
}

function GameOverView({
    snapshot,
    playerId,
    onPlayAgain
}: {
    snapshot: GameSnapshot;
    playerId: string;
    onPlayAgain: () => void;
}) {
    const me = snapshot.players.find(p => p.player_id === playerId);
    const opponent = snapshot.players.find(p => p.player_id !== playerId);

    const isWinner = snapshot.winner === playerId;
    const isDraw = !snapshot.winner && me?.score === opponent?.score;

    return (
        <View style={styles.centered}>
            <Text style={styles.gameOverTitle}>Game Over!</Text>

            <Text style={[
                styles.gameOverResult,
                isWinner ? styles.winner : (isDraw ? styles.draw : styles.loser)
            ]}>
                {isWinner ? 'You Win! 🎉' : isDraw ? "It's a Draw!" : 'You Lost'}
            </Text>

            <View style={styles.finalScore}>
                <Text style={styles.finalScoreText}>
                    {me?.score || 0} - {opponent?.score || 0}
                </Text>
            </View>

            <TouchableOpacity style={styles.button} onPress={onPlayAgain}>
                <Text style={styles.buttonText}>Play Again</Text>
            </TouchableOpacity>
        </View>
    );
}

const styles = StyleSheet.create({
    container: {
        flex: 1,
        backgroundColor: '#1a1a2e',
    },
    header: {
        flexDirection: 'row',
        justifyContent: 'center',
        alignItems: 'center',
        paddingVertical: 16,
        borderBottomWidth: 1,
        borderBottomColor: '#2d2d44',
    },
    title: {
        fontSize: 24,
        fontWeight: 'bold',
        color: '#fff',
    },
    statusDot: {
        width: 10,
        height: 10,
        borderRadius: 5,
        marginLeft: 10,
    },
    connected: {
        backgroundColor: '#00b894',
    },
    disconnected: {
        backgroundColor: '#e74c3c',
    },
    errorBanner: {
        backgroundColor: '#e74c3c',
        padding: 12,
    },
    errorText: {
        color: '#fff',
        textAlign: 'center',
    },
    centered: {
        flex: 1,
        justifyContent: 'center',
        alignItems: 'center',
        padding: 20,
    },
    subtitle: {
        fontSize: 18,
        color: '#b2bec3',
        marginBottom: 20,
    },
    input: {
        width: '80%',
        backgroundColor: '#2d2d44',
        color: '#fff',
        padding: 16,
        borderRadius: 12,
        fontSize: 18,
        marginBottom: 20,
        textAlign: 'center',
    },
    button: {
        backgroundColor: '#6c5ce7',
        paddingVertical: 16,
        paddingHorizontal: 48,
        borderRadius: 12,
    },
    buttonDisabled: {
        opacity: 0.5,
    },
    buttonText: {
        color: '#fff',
        fontSize: 18,
        fontWeight: 'bold',
    },
    buttonSecondary: {
        marginTop: 20,
        paddingVertical: 12,
        paddingHorizontal: 24,
    },
    buttonSecondaryText: {
        color: '#b2bec3',
        fontSize: 16,
    },
    loadingText: {
        color: '#b2bec3',
        marginTop: 16,
        fontSize: 16,
    },
    queuePosition: {
        color: '#b2bec3',
        marginTop: 8,
    },
    matchTitle: {
        fontSize: 32,
        fontWeight: 'bold',
        color: '#00b894',
        marginBottom: 16,
    },
    opponentName: {
        fontSize: 24,
        color: '#fff',
    },
    countdownText: {
        fontSize: 48,
        fontWeight: 'bold',
        color: '#fff',
    },
    gameContainer: {
        flex: 1,
        padding: 20,
    },
    scoreboard: {
        flexDirection: 'row',
        justifyContent: 'space-around',
        alignItems: 'center',
        marginBottom: 20,
    },
    playerScore: {
        alignItems: 'center',
    },
    playerName: {
        color: '#b2bec3',
        fontSize: 14,
    },
    playerNameHighlight: {
        color: '#6c5ce7',
    },
    score: {
        color: '#fff',
        fontSize: 32,
        fontWeight: 'bold',
    },
    scoreHighlight: {
        color: '#6c5ce7',
    },
    vs: {
        color: '#636e72',
        fontSize: 16,
    },
    roundInfo: {
        flexDirection: 'row',
        justifyContent: 'space-between',
        alignItems: 'center',
        marginBottom: 30,
    },
    roundText: {
        color: '#b2bec3',
        fontSize: 16,
    },
    timer: {
        color: '#00b894',
        fontSize: 24,
        fontWeight: 'bold',
    },
    timerWarning: {
        color: '#e74c3c',
    },
    questionContainer: {
        backgroundColor: '#2d2d44',
        padding: 40,
        borderRadius: 20,
        alignItems: 'center',
        marginBottom: 30,
    },
    questionText: {
        color: '#fff',
        fontSize: 48,
        fontWeight: 'bold',
    },
    answerContainer: {
        flexDirection: 'row',
        gap: 12,
    },
    answerInput: {
        flex: 1,
        backgroundColor: '#2d2d44',
        color: '#fff',
        padding: 16,
        borderRadius: 12,
        fontSize: 24,
        textAlign: 'center',
    },
    submitButton: {
        backgroundColor: '#6c5ce7',
        padding: 16,
        borderRadius: 12,
        justifyContent: 'center',
    },
    submitButtonText: {
        color: '#fff',
        fontSize: 18,
        fontWeight: 'bold',
    },
    waitingContainer: {
        alignItems: 'center',
        padding: 20,
    },
    waitingText: {
        color: '#00b894',
        fontSize: 18,
    },
    resultTitle: {
        fontSize: 24,
        color: '#fff',
        marginBottom: 20,
    },
    correctAnswer: {
        fontSize: 32,
        color: '#00b894',
        fontWeight: 'bold',
        marginBottom: 16,
    },
    resultCorrect: {
        fontSize: 20,
        color: '#00b894',
    },
    resultWrong: {
        fontSize: 20,
        color: '#e74c3c',
    },
    nextRound: {
        color: '#b2bec3',
        marginTop: 20,
    },
    gameOverTitle: {
        fontSize: 36,
        fontWeight: 'bold',
        color: '#fff',
        marginBottom: 20,
    },
    gameOverResult: {
        fontSize: 28,
        fontWeight: 'bold',
        marginBottom: 20,
    },
    winner: {
        color: '#00b894',
    },
    draw: {
        color: '#f39c12',
    },
    loser: {
        color: '#e74c3c',
    },
    finalScore: {
        marginBottom: 30,
    },
    finalScoreText: {
        fontSize: 48,
        fontWeight: 'bold',
        color: '#fff',
    },
});
