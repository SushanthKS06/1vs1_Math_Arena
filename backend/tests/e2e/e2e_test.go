package e2e

import (
	"encoding/json"
	"fmt"
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

// TestFullGameE2E runs a complete end-to-end test of a full game
func TestFullGameE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Setup
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Connect two players
	p1 := connectPlayer(t, server.URL, "e2e_player_1", "Player 1")
	defer p1.Close()

	p2 := connectPlayer(t, server.URL, "e2e_player_2", "Player 2")
	defer p2.Close()

	// Join queue
	sendMessage(t, p1, "JOIN_QUEUE", map[string]string{
		"player_id":    "e2e_player_1",
		"display_name": "Player 1",
	})
	sendMessage(t, p2, "JOIN_QUEUE", map[string]string{
		"player_id":    "e2e_player_2",
		"display_name": "Player 2",
	})

	// Wait for match
	gameID := waitForMatch(t, p1, 10*time.Second)
	t.Logf("Match created: %s", gameID)

	// Drain p2's match message
	_ = waitForMatch(t, p2, 5*time.Second)

	// Play through the game
	playGame(t, p1, p2, gameID)

	// Verify game ended properly
	t.Log("E2E test completed successfully")
}

// TestReconnectionE2E tests the reconnection flow
func TestReconnectionE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Connect and start game
	p1 := connectPlayer(t, server.URL, "recon_player_1", "Player 1")
	p2 := connectPlayer(t, server.URL, "recon_player_2", "Player 2")
	defer p2.Close()

	sendMessage(t, p1, "JOIN_QUEUE", map[string]string{
		"player_id": "recon_player_1", "display_name": "Player 1",
	})
	sendMessage(t, p2, "JOIN_QUEUE", map[string]string{
		"player_id": "recon_player_2", "display_name": "Player 2",
	})

	gameID := waitForMatch(t, p1, 10*time.Second)
	_ = waitForMatch(t, p2, 5*time.Second)

	// Wait for first round to start
	waitForState(t, p1, "round_active", 10*time.Second)

	// Disconnect player 1
	p1.Close()
	t.Log("Player 1 disconnected")

	// Wait a bit
	time.Sleep(2 * time.Second)

	// Reconnect player 1
	p1Recon := connectPlayer(t, server.URL, "recon_player_1", "Player 1")
	defer p1Recon.Close()

	sendMessage(t, p1Recon, "RECONNECT", map[string]string{
		"player_id": "recon_player_1",
		"game_id":   gameID,
	})

	// Should receive snapshot
	snapshot := waitForSnapshot(t, p1Recon, 5*time.Second)
	if snapshot.GameID != gameID {
		t.Errorf("got wrong game on reconnect: %s vs %s", snapshot.GameID, gameID)
	}

	t.Logf("Reconnection successful, state: %s", snapshot.State)
}

// TestConcurrentMatchmakingE2E tests multiple concurrent matches
func TestConcurrentMatchmakingE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create 20 players (10 games)
	numPairs := 10
	var wg sync.WaitGroup
	results := make(chan string, numPairs)

	for i := 0; i < numPairs; i++ {
		wg.Add(2)

		// Player A
		go func(idx int) {
			defer wg.Done()
			p := connectPlayer(t, server.URL, fmt.Sprintf("concurrent_a_%d", idx), "A")
			defer p.Close()

			sendMessage(t, p, "JOIN_QUEUE", map[string]string{
				"player_id":    fmt.Sprintf("concurrent_a_%d", idx),
				"display_name": "A",
			})

			gameID := waitForMatch(t, p, 30*time.Second)
			results <- gameID
		}(i)

		// Player B
		go func(idx int) {
			defer wg.Done()
			p := connectPlayer(t, server.URL, fmt.Sprintf("concurrent_b_%d", idx), "B")
			defer p.Close()

			sendMessage(t, p, "JOIN_QUEUE", map[string]string{
				"player_id":    fmt.Sprintf("concurrent_b_%d", idx),
				"display_name": "B",
			})

			_ = waitForMatch(t, p, 30*time.Second)
		}(i)
	}

	wg.Wait()
	close(results)

	// Verify all matches created
	gameIDs := make(map[string]bool)
	for gameID := range results {
		gameIDs[gameID] = true
	}

	if len(gameIDs) != numPairs {
		t.Errorf("expected %d unique games, got %d", numPairs, len(gameIDs))
	}

	t.Logf("Created %d concurrent games successfully", len(gameIDs))
}

// TestAnswerValidationE2E tests various answer scenarios
func TestAnswerValidationE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	server, cleanup := setupTestServer(t)
	defer cleanup()

	p1 := connectPlayer(t, server.URL, "val_player_1", "P1")
	defer p1.Close()
	p2 := connectPlayer(t, server.URL, "val_player_2", "P2")
	defer p2.Close()

	sendMessage(t, p1, "JOIN_QUEUE", map[string]string{"player_id": "val_player_1", "display_name": "P1"})
	sendMessage(t, p2, "JOIN_QUEUE", map[string]string{"player_id": "val_player_2", "display_name": "P2"})

	gameID := waitForMatch(t, p1, 10*time.Second)
	_ = waitForMatch(t, p2, 5*time.Second)

	// Wait for round active
	snapshot := waitForState(t, p1, "round_active", 10*time.Second)

	// Test 1: Valid answer
	sendMessage(t, p1, "ANSWER", map[string]interface{}{
		"game_id":   gameID,
		"round":     snapshot.Round,
		"answer":    42, // Any answer
		"client_ts": time.Now().UnixMilli(),
	})

	ack1 := waitForAnswerAck(t, p1, 5*time.Second)
	if !ack1.Accepted {
		t.Error("first answer should be accepted")
	}

	// Test 2: Duplicate answer (same player, same round)
	sendMessage(t, p1, "ANSWER", map[string]interface{}{
		"game_id":   gameID,
		"round":     snapshot.Round,
		"answer":    42,
		"client_ts": time.Now().UnixMilli(),
	})

	ack2 := waitForAnswerAck(t, p1, 5*time.Second)
	if ack2.Accepted {
		t.Error("duplicate answer should be rejected")
	}
	if ack2.Reason != "already_answered" {
		t.Errorf("expected reason 'already_answered', got '%s'", ack2.Reason)
	}

	t.Log("Answer validation tests passed")
}

// Helper functions

func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	logger := zerolog.Nop()
	cfg := config.Load()

	rdb, err := redis.NewClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	gm := game.NewGameManager(game.ManagerConfig{
		TotalRounds:      3,
		RoundDuration:    5 * time.Second,
		GracePeriod:      10 * time.Second,
		CountdownSeconds: 1,
		Difficulty:       1,
	}, rdb, logger)

	mm := matchmaker.NewMatchmaker(rdb, gm, logger)
	mm.Start()

	hub := ws.NewHub(gm, mm, rdb, logger)
	go hub.Run()

	mm.SetOnMatchFound(hub.SendToPlayer)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playerID := r.URL.Query().Get("player_id")
		displayName := r.URL.Query().Get("display_name")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := ws.NewClient(hub, conn, playerID, displayName, logger)
		hub.Register(client)
		go client.ReadPump()
		go client.WritePump()
	}))

	cleanup := func() {
		mm.Stop()
		rdb.Close()
		server.Close()
	}

	return server, cleanup
}

func connectPlayer(t *testing.T, serverURL, playerID, displayName string) *websocket.Conn {
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") +
		fmt.Sprintf("?player_id=%s&display_name=%s", playerID, displayName)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	return conn
}

func sendMessage(t *testing.T, conn *websocket.Conn, msgType string, payload interface{}) {
	payloadBytes, _ := json.Marshal(payload)
	msg := protocol.Message{
		Type:    msgType,
		Payload: payloadBytes,
		TS:      time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}
}

func readMessage(conn *websocket.Conn, timeout time.Duration) (*protocol.Message, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var msg protocol.Message
	json.Unmarshal(data, &msg)
	return &msg, nil
}

func waitForMatch(t *testing.T, conn *websocket.Conn, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := readMessage(conn, timeout)
		if err != nil {
			continue
		}
		if msg.Type == "MATCH_FOUND" {
			var payload protocol.MatchFoundPayload
			json.Unmarshal(msg.Payload, &payload)
			return payload.GameID
		}
	}
	t.Fatal("timeout waiting for match")
	return ""
}

func waitForSnapshot(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.GameSnapshot {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := readMessage(conn, timeout)
		if err != nil {
			continue
		}
		if msg.Type == "GAME_SNAPSHOT" {
			var snapshot protocol.GameSnapshot
			json.Unmarshal(msg.Payload, &snapshot)
			return &snapshot
		}
	}
	t.Fatal("timeout waiting for snapshot")
	return nil
}

func waitForState(t *testing.T, conn *websocket.Conn, state string, timeout time.Duration) *protocol.GameSnapshot {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := readMessage(conn, timeout)
		if err != nil {
			continue
		}
		if msg.Type == "GAME_SNAPSHOT" {
			var snapshot protocol.GameSnapshot
			json.Unmarshal(msg.Payload, &snapshot)
			if snapshot.State == state {
				return &snapshot
			}
		}
	}
	t.Fatalf("timeout waiting for state: %s", state)
	return nil
}

func waitForAnswerAck(t *testing.T, conn *websocket.Conn, timeout time.Duration) *protocol.AnswerAckPayload {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := readMessage(conn, timeout)
		if err != nil {
			continue
		}
		if msg.Type == "ANSWER_ACK" {
			var ack protocol.AnswerAckPayload
			json.Unmarshal(msg.Payload, &ack)
			return &ack
		}
	}
	t.Fatal("timeout waiting for answer ack")
	return nil
}

func playGame(t *testing.T, p1, p2 *websocket.Conn, gameID string) {
	// Simple game play - just submit answers for each round
	for round := 1; round <= 3; round++ {
		// Wait for round active
		_ = waitForState(t, p1, "round_active", 10*time.Second)

		// Both players submit
		sendMessage(t, p1, "ANSWER", map[string]interface{}{
			"game_id": gameID, "round": round, "answer": 42, "client_ts": time.Now().UnixMilli(),
		})
		sendMessage(t, p2, "ANSWER", map[string]interface{}{
			"game_id": gameID, "round": round, "answer": 43, "client_ts": time.Now().UnixMilli(),
		})

		// Wait for round_end
		_ = waitForState(t, p1, "round_end", 10*time.Second)
	}

	// Wait for game_over
	_ = waitForState(t, p1, "game_over", 10*time.Second)
}
