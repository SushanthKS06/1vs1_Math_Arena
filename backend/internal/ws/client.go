package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mentalarena/backend/internal/protocol"
	"github.com/rs/zerolog"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	sendCh chan []byte

	PlayerID    string
	DisplayName string

	mu        sync.Mutex
	connected bool

	tokens     float64
	lastRefill time.Time
	refillRate float64
	bucketSize float64

	logger zerolog.Logger
}

func NewClient(hub *Hub, conn *websocket.Conn, playerID, displayName string, logger zerolog.Logger) *Client {
	return &Client{
		hub:         hub,
		conn:        conn,
		sendCh:      make(chan []byte, 256),
		PlayerID:    playerID,
		DisplayName: displayName,
		connected:   true,
		tokens:      20.0,
		lastRefill:  time.Now(),
		refillRate:  20.0,
		bucketSize:  20.0,
		logger:      logger.With().Str("player_id", playerID).Logger(),
	}
}

func (c *Client) ReadPump() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error().Interface("panic", r).Msg("panic_in_read_pump_recovered")
		}
		c.hub.unregisterCh <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Warn().Err(err).Msg("websocket_read_error")
			}
			return
		}

		if c.isRateLimited() {
			c.sendError("rate_limited", "Too many messages")
			continue
		}

		var msg protocol.Message
		if err := json.Unmarshal(message, &msg); err != nil {
			c.sendError("invalid_message", "Failed to parse message")
			continue
		}

		c.hub.HandleMessage(c, &msg)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.sendCh:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.sendCh)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.sendCh)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Send(msg *protocol.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed_to_marshal_message")
		return
	}

	select {
	case c.sendCh <- data:
	default:
		c.logger.Warn().Msg("send_channel_full")
	}
}

func (c *Client) SendBytes(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		c.logger.Warn().Msg("send_channel_full")
	}
}

func (c *Client) sendError(code, message string) {
	msg := protocol.MustMessage(protocol.MsgError, protocol.ErrorPayload{
		Code:    code,
		Message: message,
	})
	c.Send(msg)
}

func (c *Client) isRateLimited() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(c.lastRefill).Seconds()

	c.tokens += elapsed * c.refillRate
	if c.tokens > c.bucketSize {
		c.tokens = c.bucketSize
	}
	c.lastRefill = now

	if c.tokens >= 1.0 {
		c.tokens -= 1.0
		return false
	}

	return true
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		c.connected = false
		close(c.sendCh)
	}
}

func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}
