package game

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mentalarena/backend/internal/metrics"
	"github.com/mentalarena/backend/internal/protocol"
	"github.com/rs/zerolog"
)

type ManagerConfig struct {
	TotalRounds      int
	RoundDuration    time.Duration
	GracePeriod      time.Duration
	CountdownSeconds int
	Difficulty       int
}

type GameManager struct {
	mu       sync.RWMutex
	sessions map[string]*GameSession

	playerGames map[string]string

	cfg ManagerConfig

	sendToPlayer func(playerID string, msg *protocol.Message)

	shutdownCh chan struct{}
	wg         sync.WaitGroup

	logger zerolog.Logger
}

func NewGameManager(cfg ManagerConfig, logger zerolog.Logger) *GameManager {
	return &GameManager{
		sessions:    make(map[string]*GameSession),
		playerGames: make(map[string]string),
		cfg:         cfg,
		shutdownCh:  make(chan struct{}),
		logger:      logger.With().Str("component", "game_manager").Logger(),
	}
}

func (gm *GameManager) SetSendCallback(fn func(playerID string, msg *protocol.Message)) {
	gm.sendToPlayer = fn
}

func (gm *GameManager) CreateGame(p1, p2 PlayerInfo) (*GameSession, error) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	gameID := uuid.New().String()
	seed := time.Now().UnixNano()

	sessionCfg := SessionConfig{
		GameID:           gameID,
		Seed:             seed,
		TotalRounds:      gm.cfg.TotalRounds,
		RoundDuration:    gm.cfg.RoundDuration,
		GracePeriod:      gm.cfg.GracePeriod,
		CountdownSeconds: gm.cfg.CountdownSeconds,
		Difficulty:       gm.cfg.Difficulty,
		Logger:           gm.logger,
	}

	session := NewGameSession(sessionCfg, p1, p2)
	session.SetCallbacks(gm.handleSnapshot, gm.handleGameEnd)

	gm.sessions[gameID] = session
	gm.playerGames[p1.PlayerID] = gameID
	gm.playerGames[p2.PlayerID] = gameID

	metrics.MatchesCreated.Inc()
	metrics.ActiveGames.Inc()

	gm.logger.Info().
		Str("game_id", gameID).
		Str("player1", p1.PlayerID).
		Str("player2", p2.PlayerID).
		Msg("game_created")

	return session, nil
}

func (gm *GameManager) StartGame(gameID string) error {
	gm.mu.RLock()
	session, exists := gm.sessions[gameID]
	gm.mu.RUnlock()

	if !exists {
		return ErrGameNotFound
	}

	session.Start()
	return nil
}

func (gm *GameManager) SubmitAnswer(playerID string, payload protocol.AnswerPayload) AnswerResult {
	gm.mu.RLock()
	session, exists := gm.sessions[payload.GameID]
	gm.mu.RUnlock()

	if !exists {
		return AnswerResult{Accepted: false, Reason: "game_not_found"}
	}

	if !session.HasPlayer(playerID) {
		return AnswerResult{Accepted: false, Reason: "not_in_game"}
	}

	responseCh := make(chan ActionResult, 1)
	session.SubmitAction(GameAction{
		Type:       ActionAnswer,
		PlayerID:   playerID,
		Data:       payload,
		ResponseCh: responseCh,
	})

	select {
	case result := <-responseCh:
		return result.Data.(AnswerResult)
	case <-time.After(5 * time.Second):
		return AnswerResult{Accepted: false, Reason: "timeout"}
	}
}

func (gm *GameManager) HandleDisconnect(playerID string) {
	gm.mu.RLock()
	gameID, exists := gm.playerGames[playerID]
	if !exists {
		gm.mu.RUnlock()
		return
	}
	session, exists := gm.sessions[gameID]
	gm.mu.RUnlock()

	if !exists {
		return
	}

	session.SubmitAction(GameAction{
		Type:     ActionDisconnect,
		PlayerID: playerID,
	})
}

func (gm *GameManager) HandleReconnect(playerID, gameID string) (*protocol.GameSnapshot, error) {
	gm.mu.RLock()
	session, exists := gm.sessions[gameID]
	gm.mu.RUnlock()

	if !exists {
		return nil, ErrGameNotFound
	}

	if !session.HasPlayer(playerID) {
		return nil, ErrPlayerNotInGame
	}

	if !session.IsActive() {
		return nil, ErrGameNotActive
	}

	session.SubmitAction(GameAction{
		Type:     ActionReconnect,
		PlayerID: playerID,
	})

	return session.GetSnapshot(), nil
}

func (gm *GameManager) GetPlayerGame(playerID string) (string, bool) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	gameID, exists := gm.playerGames[playerID]
	return gameID, exists
}

func (gm *GameManager) GetSession(gameID string) (*GameSession, bool) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	session, exists := gm.sessions[gameID]
	return session, exists
}

func (gm *GameManager) ActiveGameCount() int {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return len(gm.sessions)
}

func (gm *GameManager) GracefulShutdown(ctx context.Context) error {
	close(gm.shutdownCh)

	gm.logger.Info().Int("active_games", gm.ActiveGameCount()).Msg("starting_graceful_shutdown")

	done := make(chan struct{})
	go func() {
		for {
			if gm.ActiveGameCount() == 0 {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-done:
		gm.logger.Info().Msg("graceful_shutdown_complete")
		return nil
	case <-ctx.Done():
		gm.mu.Lock()
		for _, session := range gm.sessions {
			session.Stop()
		}
		gm.mu.Unlock()
		gm.logger.Warn().Msg("graceful_shutdown_timeout_force_stopped")
		return ctx.Err()
	}
}

func (gm *GameManager) handleSnapshot(gameID string, playerID string, snapshot *protocol.GameSnapshot) {
	if gm.sendToPlayer == nil {
		return
	}

	msg := protocol.MustMessage(protocol.MsgGameSnapshot, snapshot)
	gm.sendToPlayer(playerID, msg)
}

func (gm *GameManager) handleGameEnd(gameID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	session, exists := gm.sessions[gameID]
	if !exists {
		return
	}

	switch session.State {
	case StateGameOver:
		metrics.RecordMatchCompleted("normal")
	case StateAbandoned:
		metrics.RecordMatchCompleted("abandoned")
	default:
		metrics.RecordMatchCompleted("unknown")
	}

	for _, player := range session.Players {
		delete(gm.playerGames, player.PlayerID)
	}

	delete(gm.sessions, gameID)

	gm.logger.Info().Str("game_id", gameID).Msg("game_ended_and_cleaned_up")
}

var (
	ErrGameNotFound    = &GameError{Code: "game_not_found", Message: "Game not found"}
	ErrPlayerNotInGame = &GameError{Code: "player_not_in_game", Message: "Player not in this game"}
	ErrGameNotActive   = &GameError{Code: "game_not_active", Message: "Game is not active"}
)

type GameError struct {
	Code    string
	Message string
}

func (e *GameError) Error() string {
	return e.Message
}
