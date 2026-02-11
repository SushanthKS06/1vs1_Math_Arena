package game

import (
	"sync"
	"testing"
	"time"

	"github.com/mentalarena/backend/internal/protocol"
	"github.com/rs/zerolog"
)

func TestConcurrentAnswerSubmission(t *testing.T) {
	logger := zerolog.Nop()

	session := &GameSession{
		GameID:       "concurrent-test",
		Seed:         12345,
		State:        StateRoundActive,
		CurrentRound: 1,
		TotalRounds:  10,
		Players: [2]*PlayerState{
			{PlayerID: "p1", DisplayName: "Player 1", Score: 0, Connected: true},
			{PlayerID: "p2", DisplayName: "Player 2", Score: 0, Connected: true},
		},
		Rounds:        []*Round{NewRound(1, Question{ID: 1, Expression: "5+3", Answer: 8}, 10*time.Second)},
		RoundDuration: 10 * time.Second,
		logger:        logger,
	}

	var wg sync.WaitGroup
	results := make([]AnswerResult, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()
		results[0] = session.processAnswer("p1", protocol.AnswerPayload{
			Round:    1,
			Answer:   8,
			ClientTS: time.Now().UnixMilli(),
		})
	}()

	go func() {
		defer wg.Done()
		results[1] = session.processAnswer("p2", protocol.AnswerPayload{
			Round:    1,
			Answer:   7,
			ClientTS: time.Now().UnixMilli(),
		})
	}()

	wg.Wait()

	if !results[0].Accepted {
		t.Errorf("p1 answer should be accepted, got reason: %s", results[0].Reason)
	}
	if !results[1].Accepted {
		t.Errorf("p2 answer should be accepted, got reason: %s", results[1].Reason)
	}

	if !session.Rounds[0].AllAnswered() {
		t.Error("round should have both answers")
	}
}

func TestConcurrentDuplicateSubmission(t *testing.T) {
	logger := zerolog.Nop()

	session := &GameSession{
		GameID:       "duplicate-test",
		Seed:         12345,
		State:        StateRoundActive,
		CurrentRound: 1,
		TotalRounds:  10,
		Players: [2]*PlayerState{
			{PlayerID: "p1", DisplayName: "Player 1", Score: 0, Connected: true},
			{PlayerID: "p2", DisplayName: "Player 2", Score: 0, Connected: true},
		},
		Rounds:        []*Round{NewRound(1, Question{ID: 1, Expression: "5+3", Answer: 8}, 10*time.Second)},
		RoundDuration: 10 * time.Second,
		logger:        logger,
	}

	var wg sync.WaitGroup
	results := make([]AnswerResult, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = session.processAnswer("p1", protocol.AnswerPayload{
				Round:    1,
				Answer:   8,
				ClientTS: time.Now().UnixMilli(),
			})
		}(i)
	}

	wg.Wait()

	accepted := 0
	for _, r := range results {
		if r.Accepted {
			accepted++
		}
	}

	if accepted != 1 {
		t.Errorf("expected exactly 1 accepted answer, got %d", accepted)
	}
}

func TestRaceConditionOnRoundFinalization(t *testing.T) {
	logger := zerolog.Nop()

	session := &GameSession{
		GameID:       "finalize-test",
		Seed:         12345,
		State:        StateRoundActive,
		CurrentRound: 1,
		TotalRounds:  3,
		Players: [2]*PlayerState{
			{PlayerID: "p1", DisplayName: "Player 1", Score: 0, Connected: true},
			{PlayerID: "p2", DisplayName: "Player 2", Score: 0, Connected: true},
		},
		Rounds:        []*Round{NewRound(1, Question{ID: 1, Expression: "2+2", Answer: 4}, 10*time.Second)},
		RoundDuration: 10 * time.Second,
		logger:        logger,
	}

	session.Rounds[0].Answers["p1"] = &PlayerAnswer{Answer: 4, SubmittedAt: time.Now(), Correct: true}
	session.Rounds[0].Answers["p2"] = &PlayerAnswer{Answer: 5, SubmittedAt: time.Now(), Correct: false}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.finalizeCurrentRound()
		}()
	}
	wg.Wait()

	if session.Players[0].Score != 1 {
		t.Errorf("expected p1 score 1, got %d (possible double-scoring)", session.Players[0].Score)
	}
	if session.Players[1].Score != 0 {
		t.Errorf("expected p2 score 0, got %d", session.Players[1].Score)
	}
}

func TestGracePeriodWithGameEnd(t *testing.T) {
	logger := zerolog.Nop()

	session := &GameSession{
		GameID:           "grace-test",
		Seed:             12345,
		State:            StateGameOver,
		CurrentRound:     10,
		TotalRounds:      10,
		countdownSeconds: 3,
		Players: [2]*PlayerState{
			{PlayerID: "p1", DisplayName: "Player 1", Score: 5, Connected: true},
			{PlayerID: "p2", DisplayName: "Player 2", Score: 5, Connected: false},
		},
		disconnectTimes: map[string]time.Time{"p2": time.Now()},
		gracePeriod:     100 * time.Millisecond,
		actionCh:        make(chan GameAction, 100),
		stopCh:          make(chan struct{}),
		logger:          logger,
	}

	session.forfeitGame("p2")

	if session.State != StateGameOver {
		t.Errorf("expected state GameOver, got %s", session.State)
	}
}

func TestDisconnectReconnectFlow(t *testing.T) {

	logger := zerolog.Nop()

	session := &GameSession{
		GameID:           "reconnect-test",
		Seed:             12345,
		State:            StateRoundActive,
		CurrentRound:     1,
		TotalRounds:      10,
		countdownSeconds: 3,
		Players: [2]*PlayerState{
			{PlayerID: "p1", DisplayName: "Player 1", Score: 0, Connected: true},
			{PlayerID: "p2", DisplayName: "Player 2", Score: 0, Connected: true},
		},
		Rounds:          []*Round{NewRound(1, Question{ID: 1, Expression: "5+3", Answer: 8}, 10*time.Second)},
		RoundDuration:   10 * time.Second,
		disconnectTimes: make(map[string]time.Time),
		gracePeriod:     30 * time.Second,
		actionCh:        make(chan GameAction, 100),
		stopCh:          make(chan struct{}),
		logger:          logger,
	}

	graceTickers := make(map[string]*time.Timer)

	session.handleDisconnect("p1", graceTickers)

	if session.Players[0].Connected {
		t.Error("p1 should be marked as disconnected")
	}

	if _, exists := graceTickers["p1"]; !exists {
		t.Error("grace timer should have been started for p1")
	}

	session.handleReconnect("p1", graceTickers)

	if !session.Players[0].Connected {
		t.Error("p1 should be marked as connected after reconnect")
	}

	if _, exists := graceTickers["p1"]; exists {
		t.Error("grace timer should have been cancelled for p1")
	}
}
