package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mentalarena/backend/internal/config"
	"github.com/mentalarena/backend/internal/game"
	"github.com/mentalarena/backend/internal/matchmaker"
	"github.com/mentalarena/backend/internal/protocol"
	"github.com/mentalarena/backend/internal/redis"
	"github.com/mentalarena/backend/internal/ws"
	"github.com/rs/zerolog"
)

func TestFullGameFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zerolog.Nop()
	cfg := config.Load()

	rdb, err := redis.NewClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer rdb.Close()

	defer rdb.Del("mm:queue", "mm:queue:test_p1", "mm:queue:test_p2", "lock:match:test_p1", "lock:match:test_p2")

	gm := game.NewGameManager(game.ManagerConfig{
		TotalRounds:   3,
		RoundDuration: 2 * time.Second,
		GracePeriod:   5 * time.Second,
		Difficulty:    1,
	}, logger)

	mm := matchmaker.NewMatchmaker(rdb, gm, logger)
	mm.Start()
	defer mm.Stop()

	hub := ws.NewHub(gm, mm, logger)
	go hub.Run()

	mm.SetOnMatchFound(hub.SendToPlayer)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playerID := r.URL.Query().Get("player_id")
		displayName := r.URL.Query().Get("display_name")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}

		client := ws.NewClient(hub, conn, playerID, displayName, logger)
		hub.Register(client)
		go client.ReadPump()
		go client.WritePump()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p1Conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL+"?player_id=test_p1&display_name=P1", nil)
	if err != nil {
		t.Fatalf("p1 connect failed: %v", err)
	}
	defer p1Conn.Close()

	p2Conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL+"?player_id=test_p2&display_name=P2", nil)
	if err != nil {
		t.Fatalf("p2 connect failed: %v", err)
	}
	defer p2Conn.Close()

	receiveMsg := func(conn *websocket.Conn) (*protocol.Message, error) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var msg protocol.Message
		err = json.Unmarshal(data, &msg)
		return &msg, err
	}

	sendMsg := func(conn *websocket.Conn, msgType string, payload interface{}) error {
		payloadBytes, _ := json.Marshal(payload)
		msg := protocol.Message{
			Type:    msgType,
			Payload: payloadBytes,
			TS:      time.Now().UnixMilli(),
		}
		data, _ := json.Marshal(msg)
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	sendMsg(p1Conn, "JOIN_QUEUE", protocol.JoinQueuePayload{DisplayName: "Player1"})
	sendMsg(p2Conn, "JOIN_QUEUE", protocol.JoinQueuePayload{DisplayName: "Player2"})

	var wg sync.WaitGroup
	var p1GameID, p2GameID string
	var matchErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			msg, err := receiveMsg(p1Conn)
			if err != nil {
				matchErr = err
				return
			}
			if msg.Type == "MATCH_FOUND" {
				var payload protocol.MatchFoundPayload
				json.Unmarshal(msg.Payload, &payload)
				p1GameID = payload.GameID
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			msg, err := receiveMsg(p2Conn)
			if err != nil {
				matchErr = err
				return
			}
			if msg.Type == "MATCH_FOUND" {
				var payload protocol.MatchFoundPayload
				json.Unmarshal(msg.Payload, &payload)
				p2GameID = payload.GameID
				return
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for match")
	}

	if matchErr != nil {
		t.Fatalf("match error: %v", matchErr)
	}

	if p1GameID != p2GameID {
		t.Errorf("game IDs don't match: %s vs %s", p1GameID, p2GameID)
	}

	t.Logf("Match created with game ID: %s", p1GameID)
}

func TestAnswerValidationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Log("Answer validation integration test placeholder")
}

func TestReconnectionIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Log("Reconnection integration test placeholder")
}
