package game

import (
	"testing"
	"time"

	"github.com/mentalarena/backend/internal/protocol"
	"github.com/rs/zerolog"
)

func TestAnswerValidation(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name     string
		setup    func(*GameSession)
		playerID string
		payload  protocol.AnswerPayload
		expected string
	}{
		{
			name: "valid_answer",
			setup: func(s *GameSession) {
				s.State = StateRoundActive
				s.CurrentRound = 1
				s.Rounds = []*Round{NewRound(1, Question{ID: 1, Expression: "2+2", Answer: 4}, 10*time.Second)}
			},
			playerID: "player1",
			payload:  protocol.AnswerPayload{Round: 1, Answer: 4},
			expected: "",
		},
		{
			name: "wrong_round",
			setup: func(s *GameSession) {
				s.State = StateRoundActive
				s.CurrentRound = 2
				s.Rounds = []*Round{
					NewRound(1, Question{ID: 1, Expression: "2+2", Answer: 4}, 10*time.Second),
					NewRound(2, Question{ID: 2, Expression: "3+3", Answer: 6}, 10*time.Second),
				}
			},
			playerID: "player1",
			payload:  protocol.AnswerPayload{Round: 1, Answer: 4},
			expected: "wrong_round",
		},
		{
			name: "game_not_active",
			setup: func(s *GameSession) {
				s.State = StateCountdown
				s.CurrentRound = 0
			},
			playerID: "player1",
			payload:  protocol.AnswerPayload{Round: 1, Answer: 4},
			expected: "game_not_active",
		},
		{
			name: "duplicate_answer",
			setup: func(s *GameSession) {
				s.State = StateRoundActive
				s.CurrentRound = 1
				s.Rounds = []*Round{NewRound(1, Question{ID: 1, Expression: "2+2", Answer: 4}, 10*time.Second)}
				s.Rounds[0].Answers["player1"] = &PlayerAnswer{Answer: 4, SubmittedAt: time.Now(), Correct: true}
			},
			playerID: "player1",
			payload:  protocol.AnswerPayload{Round: 1, Answer: 4},
			expected: "already_answered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &GameSession{
				GameID:      "test-game",
				TotalRounds: 10,
				Players: [2]*PlayerState{
					{PlayerID: "player1", DisplayName: "P1", Score: 0, Connected: true},
					{PlayerID: "player2", DisplayName: "P2", Score: 0, Connected: true},
				},
				RoundDuration: 10 * time.Second,
				logger:        logger,
			}
			tt.setup(session)

			result := session.processAnswer(tt.playerID, tt.payload)

			if tt.expected == "" {
				if !result.Accepted {
					t.Errorf("expected answer to be accepted, got reason: %s", result.Reason)
				}
			} else {
				if result.Accepted {
					t.Errorf("expected answer to be rejected with reason %s", tt.expected)
				}
				if result.Reason != tt.expected {
					t.Errorf("expected reason %s, got %s", tt.expected, result.Reason)
				}
			}
		})
	}
}

func TestQuestionGeneratorDeterminism(t *testing.T) {
	seed := int64(12345)

	gen1 := NewQuestionGenerator(seed, 2)
	gen2 := NewQuestionGenerator(seed, 2)

	for i := 1; i <= 10; i++ {
		q1 := gen1.Generate(i)
		q2 := gen2.Generate(i)

		if q1.Expression != q2.Expression {
			t.Errorf("round %d: expressions differ: %s vs %s", i, q1.Expression, q2.Expression)
		}
		if q1.Answer != q2.Answer {
			t.Errorf("round %d: answers differ: %d vs %d", i, q1.Answer, q2.Answer)
		}
	}
}

func TestRoundLifecycle(t *testing.T) {
	question := Question{ID: 1, Expression: "5 + 3", Answer: 8}
	round := NewRound(1, question, 10*time.Second)

	if round.IsExpired() {
		t.Error("round should not be expired immediately")
	}

	if round.HasAnswer("player1") {
		t.Error("player1 should not have answer initially")
	}
	if round.AllAnswered() {
		t.Error("not all players should have answered initially")
	}

	round.Answers["player1"] = &PlayerAnswer{Answer: 8, SubmittedAt: time.Now(), Correct: true}

	if !round.HasAnswer("player1") {
		t.Error("player1 should have answer now")
	}
	if round.AllAnswered() {
		t.Error("not all players should have answered yet")
	}

	round.Answers["player2"] = &PlayerAnswer{Answer: 7, SubmittedAt: time.Now(), Correct: false}

	if !round.AllAnswered() {
		t.Error("all players should have answered now")
	}
}

func TestScoreCalculation(t *testing.T) {
	logger := zerolog.Nop()

	session := &GameSession{
		GameID:       "test-game",
		Seed:         12345,
		State:        StateRoundActive,
		CurrentRound: 1,
		TotalRounds:  3,
		Players: [2]*PlayerState{
			{PlayerID: "p1", DisplayName: "Player 1", Score: 0, Connected: true},
			{PlayerID: "p2", DisplayName: "Player 2", Score: 0, Connected: true},
		},
		Rounds: []*Round{
			NewRound(1, Question{ID: 1, Expression: "2+2", Answer: 4}, 10*time.Second),
		},
		RoundDuration: 10 * time.Second,
		logger:        logger,
	}

	session.Rounds[0].Answers["p1"] = &PlayerAnswer{Answer: 4, SubmittedAt: time.Now(), Correct: true}
	session.Rounds[0].Answers["p2"] = &PlayerAnswer{Answer: 5, SubmittedAt: time.Now(), Correct: false}

	session.finalizeCurrentRoundLocked()

	if session.Players[0].Score != 1 {
		t.Errorf("expected p1 score 1, got %d", session.Players[0].Score)
	}
	if session.Players[1].Score != 0 {
		t.Errorf("expected p2 score 0, got %d", session.Players[1].Score)
	}
}
