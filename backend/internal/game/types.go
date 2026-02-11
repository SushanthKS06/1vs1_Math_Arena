package game

import (
	"time"
)

type GameState string

const (
	StateWaitingForPlayers GameState = "waiting"
	StateCountdown         GameState = "countdown"
	StateRoundActive       GameState = "round_active"
	StateRoundEnd          GameState = "round_end"
	StateGameOver          GameState = "game_over"
	StateAbandoned         GameState = "abandoned"
)

type Question struct {
	ID         int    `json:"id"`
	Expression string `json:"expression"`
	Answer     int    `json:"answer"`
}

type PlayerState struct {
	PlayerID      string
	DisplayName   string
	Score         int
	Connected     bool
	CurrentAnswer *PlayerAnswer
}

type PlayerAnswer struct {
	Answer      int
	SubmittedAt time.Time
	Correct     bool
}

type Round struct {
	Number     int
	Question   Question
	StartTime  time.Time
	DeadlineTS int64
	Answers    map[string]*PlayerAnswer
	Finalized  bool
}

func NewRound(number int, question Question, duration time.Duration) *Round {
	now := time.Now()
	return &Round{
		Number:     number,
		Question:   question,
		StartTime:  now,
		DeadlineTS: now.Add(duration).UnixMilli(),
		Answers:    make(map[string]*PlayerAnswer),
		Finalized:  false,
	}
}

func (r *Round) IsExpired() bool {
	return time.Now().UnixMilli() > r.DeadlineTS
}

func (r *Round) HasAnswer(playerID string) bool {
	_, exists := r.Answers[playerID]
	return exists
}

func (r *Round) AllAnswered() bool {
	return len(r.Answers) >= 2
}

type GameAction struct {
	Type       ActionType
	PlayerID   string
	Data       interface{}
	ResponseCh chan ActionResult
}

type ActionType int

const (
	ActionAnswer ActionType = iota
	ActionDisconnect
	ActionReconnect
	ActionTick
)

type ActionResult struct {
	Success bool
	Error   error
	Data    interface{}
}

type AnswerResult struct {
	Accepted bool
	Reason   string
}
