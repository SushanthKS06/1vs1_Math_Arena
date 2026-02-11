package ws

import (
	"encoding/json"
	"sync"

	"github.com/mentalarena/backend/internal/game"
	"github.com/mentalarena/backend/internal/matchmaker"
	"github.com/mentalarena/backend/internal/protocol"
	"github.com/rs/zerolog"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client

	registerCh   chan *Client
	unregisterCh chan *Client

	gameManager *game.GameManager
	matchmaker  *matchmaker.Matchmaker

	logger zerolog.Logger
}

func NewHub(gm *game.GameManager, mm *matchmaker.Matchmaker, logger zerolog.Logger) *Hub {
	hub := &Hub{
		clients:      make(map[string]*Client),
		registerCh:   make(chan *Client, 100),
		unregisterCh: make(chan *Client, 100),
		gameManager:  gm,
		matchmaker:   mm,
		logger:       logger.With().Str("component", "hub").Logger(),
	}

	gm.SetSendCallback(hub.SendToPlayer)

	return hub
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.registerCh:
			h.handleRegister(client)

		case client := <-h.unregisterCh:
			h.handleUnregister(client)
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, exists := h.clients[client.PlayerID]; exists {
		existing.Close()
	}

	h.clients[client.PlayerID] = client
	h.logger.Info().Str("player_id", client.PlayerID).Msg("client_registered")
}

func (h *Hub) handleUnregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, exists := h.clients[client.PlayerID]; exists && existing == client {
		delete(h.clients, client.PlayerID)
		client.Close()
		h.logger.Info().Str("player_id", client.PlayerID).Msg("client_unregistered")

		h.gameManager.HandleDisconnect(client.PlayerID)
	}
}

func (h *Hub) Register(client *Client) {
	h.registerCh <- client
}

func (h *Hub) HandleMessage(client *Client, msg *protocol.Message) {
	switch msg.Type {
	case protocol.MsgJoinQueue:
		h.handleJoinQueue(client, msg)

	case protocol.MsgLeaveQueue:
		h.handleLeaveQueue(client)

	case protocol.MsgAnswer:
		h.handleAnswer(client, msg)

	case protocol.MsgReconnect:
		h.handleReconnect(client, msg)

	case protocol.MsgPing:
		client.Send(protocol.MustMessage(protocol.MsgPong, nil))

	default:
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "unknown_message_type",
			Message: "Unknown message type: " + msg.Type,
		}))
	}
}

func (h *Hub) handleJoinQueue(client *Client, msg *protocol.Message) {
	var payload protocol.JoinQueuePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "invalid_payload",
			Message: "Invalid JOIN_QUEUE payload",
		}))
		return
	}

	if payload.DisplayName != "" {
		client.DisplayName = payload.DisplayName
	}

	if gameID, exists := h.gameManager.GetPlayerGame(client.PlayerID); exists {
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "already_in_game",
			Message: "Already in game: " + gameID,
		}))
		return
	}

	position, err := h.matchmaker.EnqueuePlayer(matchmaker.QueuedPlayer{
		PlayerID:    client.PlayerID,
		DisplayName: client.DisplayName,
	})
	if err != nil {
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "queue_error",
			Message: err.Error(),
		}))
		return
	}

	client.Send(protocol.MustMessage(protocol.MsgQueueJoined, protocol.QueueJoinedPayload{
		Position: position,
	}))

	h.logger.Info().
		Str("player_id", client.PlayerID).
		Int("position", position).
		Msg("player_joined_queue")
}

func (h *Hub) handleLeaveQueue(client *Client) {
	if err := h.matchmaker.DequeuePlayer(client.PlayerID); err != nil {
		h.logger.Warn().Err(err).Str("player_id", client.PlayerID).Msg("dequeue_error")
	}

	client.Send(protocol.MustMessage(protocol.MsgQueueLeft, nil))
}

func (h *Hub) handleAnswer(client *Client, msg *protocol.Message) {
	var payload protocol.AnswerPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "invalid_payload",
			Message: "Invalid ANSWER payload",
		}))
		return
	}

	result := h.gameManager.SubmitAnswer(client.PlayerID, payload)

	client.Send(protocol.MustMessage(protocol.MsgAnswerAck, protocol.AnswerAckPayload{
		Round:    payload.Round,
		Accepted: result.Accepted,
		Reason:   result.Reason,
	}))
}

func (h *Hub) handleReconnect(client *Client, msg *protocol.Message) {
	var payload protocol.ReconnectPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "invalid_payload",
			Message: "Invalid RECONNECT payload",
		}))
		return
	}

	snapshot, err := h.gameManager.HandleReconnect(client.PlayerID, payload.GameID)
	if err != nil {
		client.Send(protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
			Code:    "reconnect_failed",
			Message: err.Error(),
		}))
		return
	}

	client.Send(protocol.MustMessage(protocol.MsgGameSnapshot, snapshot))
	h.logger.Info().
		Str("player_id", client.PlayerID).
		Str("game_id", payload.GameID).
		Msg("player_reconnected")
}

func (h *Hub) SendToPlayer(playerID string, msg *protocol.Message) {
	h.mu.RLock()
	client, exists := h.clients[playerID]
	h.mu.RUnlock()

	if exists && client.IsConnected() {
		client.Send(msg)
	}
}

func (h *Hub) GetClient(playerID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, exists := h.clients[playerID]
	return client, exists
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
