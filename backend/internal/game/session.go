package game

import (
	"sync"
	"time"

	"github.com/mentalarena/backend/internal/metrics"
	"github.com/mentalarena/backend/internal/protocol"
	"github.com/rs/zerolog"
)

type GameSession struct {
	mu sync.RWMutex

	GameID string
	Seed   int64

	State        GameState
	CurrentRound int
	TotalRounds  int

	Players [2]*PlayerState

	Rounds        []*Round
	RoundDuration time.Duration

	countdownSeconds int

	questionGen *QuestionGenerator

	disconnectTimes map[string]time.Time
	gracePeriod     time.Duration

	actionCh chan GameAction
	stopCh   chan struct{}

	onSnapshot func(gameID string, playerID string, snapshot *protocol.GameSnapshot)
	onGameEnd  func(gameID string)

	logger zerolog.Logger
}

type SessionConfig struct {
	GameID           string
	Seed             int64
	TotalRounds      int
	RoundDuration    time.Duration
	GracePeriod      time.Duration
	CountdownSeconds int
	Difficulty       int
	Logger           zerolog.Logger
}

func NewGameSession(cfg SessionConfig, p1, p2 PlayerInfo) *GameSession {
	countdown := cfg.CountdownSeconds
	if countdown <= 0 {
		countdown = 3
	}
	return &GameSession{
		GameID:           cfg.GameID,
		Seed:             cfg.Seed,
		State:            StateWaitingForPlayers,
		CurrentRound:     0,
		TotalRounds:      cfg.TotalRounds,
		countdownSeconds: countdown,
		Players: [2]*PlayerState{
			{PlayerID: p1.PlayerID, DisplayName: p1.DisplayName, Score: 0, Connected: true},
			{PlayerID: p2.PlayerID, DisplayName: p2.DisplayName, Score: 0, Connected: true},
		},
		Rounds:          make([]*Round, 0, cfg.TotalRounds),
		RoundDuration:   cfg.RoundDuration,
		questionGen:     NewQuestionGenerator(cfg.Seed, cfg.Difficulty),
		disconnectTimes: make(map[string]time.Time),
		gracePeriod:     cfg.GracePeriod,
		actionCh:        make(chan GameAction, 100),
		stopCh:          make(chan struct{}),
		logger:          cfg.Logger.With().Str("game_id", cfg.GameID).Logger(),
	}
}

type PlayerInfo struct {
	PlayerID    string
	DisplayName string
}

func (s *GameSession) SetCallbacks(
	onSnapshot func(gameID string, playerID string, snapshot *protocol.GameSnapshot),
	onGameEnd func(gameID string),
) {
	s.onSnapshot = onSnapshot
	s.onGameEnd = onGameEnd
}

func (s *GameSession) Start() {
	go s.run()
}

func (s *GameSession) Stop() {
	close(s.stopCh)
}

func (s *GameSession) run() {
	s.logger.Info().Msg("game session started")

	s.transitionTo(StateCountdown)
	s.broadcastSnapshot()

	time.Sleep(time.Duration(s.countdownSeconds) * time.Second)

	s.startNextRound()

	roundTimer := time.NewTimer(s.RoundDuration)
	graceTickers := make(map[string]*time.Timer)

	for {
		select {
		case <-s.stopCh:
			s.logger.Info().Msg("game session stopped")
			roundTimer.Stop()
			return

		case action := <-s.actionCh:
			s.handleAction(action, graceTickers)

		case <-roundTimer.C:
			s.finalizeCurrentRound()
			if s.State == StateGameOver {
				s.cleanup(graceTickers)
				return
			}
			s.startNextRound()
			roundTimer.Reset(s.RoundDuration)
		}
	}
}

func (s *GameSession) cleanup(graceTickers map[string]*time.Timer) {
	for _, t := range graceTickers {
		t.Stop()
	}
	if s.onGameEnd != nil {
		s.onGameEnd(s.GameID)
	}
}

func (s *GameSession) handleAction(action GameAction, graceTickers map[string]*time.Timer) {
	switch action.Type {
	case ActionAnswer:
		result := s.processAnswer(action.PlayerID, action.Data.(protocol.AnswerPayload))
		if action.ResponseCh != nil {
			action.ResponseCh <- ActionResult{Success: result.Accepted, Data: result}
		}

	case ActionDisconnect:
		s.handleDisconnect(action.PlayerID, graceTickers)

	case ActionReconnect:
		s.handleReconnect(action.PlayerID, graceTickers)
	}
}

func (s *GameSession) SubmitAction(action GameAction) {
	select {
	case s.actionCh <- action:
	case <-s.stopCh:
	}
}

func (s *GameSession) processAnswer(playerID string, payload protocol.AnswerPayload) AnswerResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	latencyMs := float64(time.Now().UnixMilli() - payload.ClientTS)

	if s.State != StateRoundActive {
		metrics.RecordAnswer("invalid_state", latencyMs)
		return AnswerResult{Accepted: false, Reason: "game_not_active"}
	}

	if payload.Round != s.CurrentRound {
		metrics.RecordDesync("wrong_round")
		metrics.RecordAnswer("wrong_round", latencyMs)
		return AnswerResult{Accepted: false, Reason: "wrong_round"}
	}

	currentRound := s.Rounds[s.CurrentRound-1]

	if currentRound.IsExpired() {
		metrics.RecordDesync("late_answer")
		metrics.RecordAnswer("late", latencyMs)
		return AnswerResult{Accepted: false, Reason: "deadline_passed"}
	}

	if currentRound.HasAnswer(playerID) {
		metrics.RecordAnswer("duplicate", latencyMs)
		return AnswerResult{Accepted: false, Reason: "already_answered"}
	}

	isCorrect := payload.Answer == currentRound.Question.Answer
	currentRound.Answers[playerID] = &PlayerAnswer{
		Answer:      payload.Answer,
		SubmittedAt: time.Now(),
		Correct:     isCorrect,
	}

	if isCorrect {
		metrics.RecordAnswer("correct", latencyMs)
	} else {
		metrics.RecordAnswer("incorrect", latencyMs)
	}

	s.logger.Info().
		Str("player_id", playerID).
		Int("round", s.CurrentRound).
		Int("answer", payload.Answer).
		Bool("correct", isCorrect).
		Float64("latency_ms", latencyMs).
		Msg("answer_received")

	if currentRound.AllAnswered() {
		s.finalizeCurrentRoundLocked()
	}

	return AnswerResult{Accepted: true}
}

func (s *GameSession) startNextRound() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.CurrentRound++
	question := s.questionGen.Generate(s.CurrentRound)
	round := NewRound(s.CurrentRound, question, s.RoundDuration)
	s.Rounds = append(s.Rounds, round)
	s.State = StateRoundActive

	s.logger.Info().
		Int("round", s.CurrentRound).
		Str("expression", question.Expression).
		Msg("round_started")

	s.broadcastSnapshotLocked()
}

func (s *GameSession) finalizeCurrentRound() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finalizeCurrentRoundLocked()
}

func (s *GameSession) finalizeCurrentRoundLocked() {
	if s.CurrentRound == 0 || s.CurrentRound > len(s.Rounds) {
		return
	}

	currentRound := s.Rounds[s.CurrentRound-1]
	if currentRound.Finalized {
		return
	}
	currentRound.Finalized = true

	for playerID, answer := range currentRound.Answers {
		if answer.Correct {
			for i := range s.Players {
				if s.Players[i].PlayerID == playerID {
					s.Players[i].Score++
					break
				}
			}
		}
	}

	s.logger.Info().
		Int("round", s.CurrentRound).
		Int("p1_score", s.Players[0].Score).
		Int("p2_score", s.Players[1].Score).
		Msg("round_finalized")

	if s.CurrentRound >= s.TotalRounds {
		s.State = StateGameOver
		s.logger.Info().Msg("game_over")
	} else {
		s.State = StateRoundEnd
	}

	s.broadcastSnapshotLocked()
}

func (s *GameSession) handleDisconnect(playerID string, graceTickers map[string]*time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.Players {
		if s.Players[i].PlayerID == playerID {
			s.Players[i].Connected = false
			break
		}
	}

	s.disconnectTimes[playerID] = time.Now()
	s.logger.Warn().Str("player_id", playerID).Msg("player_disconnected")

	graceTickers[playerID] = time.AfterFunc(s.gracePeriod, func() {
		s.forfeitGame(playerID)
	})

	s.broadcastSnapshotLocked()
}

func (s *GameSession) handleReconnect(playerID string, graceTickers map[string]*time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timer, exists := graceTickers[playerID]; exists {
		timer.Stop()
		delete(graceTickers, playerID)
	}
	delete(s.disconnectTimes, playerID)

	for i := range s.Players {
		if s.Players[i].PlayerID == playerID {
			s.Players[i].Connected = true
			break
		}
	}

	s.logger.Info().Str("player_id", playerID).Msg("player_reconnected")
	s.broadcastSnapshotLocked()
}

func (s *GameSession) forfeitGame(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateGameOver || s.State == StateAbandoned {
		s.logger.Debug().Str("player_id", playerID).Msg("forfeit_ignored_game_already_ended")
		return
	}

	s.State = StateAbandoned
	s.logger.Warn().Str("player_id", playerID).Msg("game_forfeited")
	s.broadcastSnapshotLocked()

	if s.onGameEnd != nil {
		s.onGameEnd(s.GameID)
	}
}

func (s *GameSession) transitionTo(newState GameState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = newState
}

func (s *GameSession) broadcastSnapshot() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.broadcastSnapshotLocked()
}

func (s *GameSession) broadcastSnapshotLocked() {
	snapshot := s.buildSnapshotLocked()
	for _, player := range s.Players {
		if s.onSnapshot != nil {
			s.onSnapshot(s.GameID, player.PlayerID, snapshot)
		}
	}
}

func (s *GameSession) buildSnapshotLocked() *protocol.GameSnapshot {
	snapshot := &protocol.GameSnapshot{
		GameID:      s.GameID,
		State:       string(s.State),
		Round:       s.CurrentRound,
		TotalRounds: s.TotalRounds,
		ServerTS:    time.Now().UnixMilli(),
		Players:     make([]protocol.PlayerData, 2),
	}

	for i, p := range s.Players {
		hasAnswered := false
		if s.CurrentRound > 0 && s.CurrentRound <= len(s.Rounds) {
			_, hasAnswered = s.Rounds[s.CurrentRound-1].Answers[p.PlayerID]
		}
		snapshot.Players[i] = protocol.PlayerData{
			PlayerID:    p.PlayerID,
			DisplayName: p.DisplayName,
			Score:       p.Score,
			Connected:   p.Connected,
			HasAnswered: hasAnswered,
		}
	}

	if s.State == StateRoundActive && s.CurrentRound > 0 {
		currentRound := s.Rounds[s.CurrentRound-1]
		snapshot.Question = &protocol.QuestionData{
			ID:         currentRound.Question.ID,
			Expression: currentRound.Question.Expression,
		}
		snapshot.DeadlineTS = currentRound.DeadlineTS
	}

	if s.State == StateRoundEnd || s.State == StateGameOver {
		if s.CurrentRound > 0 && s.CurrentRound <= len(s.Rounds) {
			currentRound := s.Rounds[s.CurrentRound-1]
			results := &protocol.RoundResults{
				CorrectAnswer: currentRound.Question.Answer,
				PlayerAnswers: make(map[string]protocol.PlayerResult),
			}
			for playerID, answer := range currentRound.Answers {
				points := 0
				if answer.Correct {
					points = 1
				}
				results.PlayerAnswers[playerID] = protocol.PlayerResult{
					Answer:  answer.Answer,
					Correct: answer.Correct,
					Points:  points,
				}
			}
			snapshot.RoundResults = results
		}
	}

	if s.State == StateGameOver {
		if s.Players[0].Score > s.Players[1].Score {
			snapshot.Winner = &s.Players[0].PlayerID
		} else if s.Players[1].Score > s.Players[0].Score {
			snapshot.Winner = &s.Players[1].PlayerID
		}
	}

	return snapshot
}

func (s *GameSession) HasPlayer(playerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.Players {
		if p.PlayerID == playerID {
			return true
		}
	}
	return false
}

func (s *GameSession) GetSnapshot() *protocol.GameSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildSnapshotLocked()
}

func (s *GameSession) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State != StateGameOver && s.State != StateAbandoned
}
