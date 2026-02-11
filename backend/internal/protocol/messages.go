package protocol

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

var globalSeq int64

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	TS      int64           `json:"ts"`
	Seq     int64           `json:"seq,omitempty"`
}

func NewMessage(msgType string, payload interface{}) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    msgType,
		Payload: data,
		TS:      time.Now().UnixMilli(),
		Seq:     atomic.AddInt64(&globalSeq, 1),
	}, nil
}

func MustMessage(msgType string, payload interface{}) *Message {
	msg, err := NewMessage(msgType, payload)
	if err != nil {
		panic(err)
	}
	return msg
}

const (
	MsgJoinQueue  = "JOIN_QUEUE"
	MsgLeaveQueue = "LEAVE_QUEUE"
	MsgAnswer     = "ANSWER"
	MsgReconnect  = "RECONNECT"
	MsgPing       = "PING"
)

const (
	MsgQueueJoined  = "QUEUE_JOINED"
	MsgQueueLeft    = "QUEUE_LEFT"
	MsgMatchFound   = "MATCH_FOUND"
	MsgGameSnapshot = "GAME_SNAPSHOT"
	MsgAnswerAck    = "ANSWER_ACK"
	MsgError        = "ERROR"
	MsgPong         = "PONG"
)

type JoinQueuePayload struct {
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
}

type AnswerPayload struct {
	GameID   string `json:"game_id"`
	Round    int    `json:"round"`
	Answer   int    `json:"answer"`
	ClientTS int64  `json:"client_ts"`
	Nonce    string `json:"nonce,omitempty"`
}

type ReconnectPayload struct {
	PlayerID string `json:"player_id"`
	GameID   string `json:"game_id"`
}

type QueueJoinedPayload struct {
	Position int `json:"position"`
}

type MatchFoundPayload struct {
	GameID   string     `json:"game_id"`
	Opponent PlayerInfo `json:"opponent"`
	StartsAt int64      `json:"starts_at"`
}

type PlayerInfo struct {
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
}

type AnswerAckPayload struct {
	Round    int    `json:"round"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type GameSnapshot struct {
	GameID       string        `json:"game_id"`
	State        string        `json:"state"`
	Round        int           `json:"round"`
	TotalRounds  int           `json:"total_rounds"`
	Question     *QuestionData `json:"question,omitempty"`
	DeadlineTS   int64         `json:"deadline_ts,omitempty"`
	ServerTS     int64         `json:"server_ts"`
	Players      []PlayerData  `json:"players"`
	RoundResults *RoundResults `json:"round_results,omitempty"`
	Winner       *string       `json:"winner,omitempty"`
}

type QuestionData struct {
	ID         int    `json:"id"`
	Expression string `json:"expression"`
}

type PlayerData struct {
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	Score       int    `json:"score"`
	Connected   bool   `json:"connected"`
	HasAnswered bool   `json:"has_answered"`
}

type RoundResults struct {
	CorrectAnswer int                     `json:"correct_answer"`
	PlayerAnswers map[string]PlayerResult `json:"player_answers"`
}

type PlayerResult struct {
	Answer  int  `json:"answer"`
	Correct bool `json:"correct"`
	Points  int  `json:"points"`
}
