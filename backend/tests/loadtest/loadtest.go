package loadtest

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type LoadTestConfig struct {
	ServerURL       string
	NumGames        int
	RampUpDuration  time.Duration
	TestDuration    time.Duration
	AnswerDelay     time.Duration
	ReconnectChance float64
}

type LoadTestResult struct {
	TotalGames      int64
	CompletedGames  int64
	FailedGames     int64
	TotalAnswers    int64
	AcceptedAnswers int64
	RejectedAnswers int64
	Reconnections   int64
	AvgLatencyMs    float64
	MaxLatencyMs    float64
	P99LatencyMs    float64
	ErrorCount      int64
	Duration        time.Duration
}

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	TS      int64           `json:"ts"`
}

type LoadTester struct {
	config  LoadTestConfig
	results LoadTestResult
	mu      sync.Mutex

	latencies []float64
}

func NewLoadTester(cfg LoadTestConfig) *LoadTester {
	return &LoadTester{
		config:    cfg,
		latencies: make([]float64, 0, cfg.NumGames*20),
	}
}

func (lt *LoadTester) Run() (*LoadTestResult, error) {
	fmt.Printf("Starting load test: %d games over %v\n", lt.config.NumGames, lt.config.TestDuration)

	var wg sync.WaitGroup
	gameCh := make(chan int, lt.config.NumGames)

	rampInterval := lt.config.RampUpDuration / time.Duration(lt.config.NumGames)

	startTime := time.Now()

	go func() {
		for i := 0; i < lt.config.NumGames; i++ {
			gameCh <- i
			time.Sleep(rampInterval)
		}
		close(gameCh)
	}()

	numWorkers := 50
	if lt.config.NumGames < numWorkers {
		numWorkers = lt.config.NumGames
	}

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for gameNum := range gameCh {
				lt.runGame(gameNum)
			}
		}(w)
	}

	wg.Wait()
	lt.results.Duration = time.Since(startTime)

	lt.calculateLatencyStats()

	return &lt.results, nil
}

func (lt *LoadTester) runGame(gameNum int) {
	atomic.AddInt64(&lt.results.TotalGames, 1)

	p1ID := fmt.Sprintf("loadtest_p1_%d", gameNum)
	p2ID := fmt.Sprintf("loadtest_p2_%d", gameNum)

	p1Conn, err := lt.connect(p1ID)
	if err != nil {
		atomic.AddInt64(&lt.results.FailedGames, 1)
		atomic.AddInt64(&lt.results.ErrorCount, 1)
		return
	}
	defer p1Conn.Close()

	p2Conn, err := lt.connect(p2ID)
	if err != nil {
		atomic.AddInt64(&lt.results.FailedGames, 1)
		atomic.AddInt64(&lt.results.ErrorCount, 1)
		return
	}
	defer p2Conn.Close()

	lt.sendMessage(p1Conn, "JOIN_QUEUE", map[string]string{"player_id": p1ID, "display_name": "LT_P1"})
	lt.sendMessage(p2Conn, "JOIN_QUEUE", map[string]string{"player_id": p2ID, "display_name": "LT_P2"})

	var gameID string
	timeout := time.After(30 * time.Second)

	for gameID == "" {
		select {
		case <-timeout:
			atomic.AddInt64(&lt.results.FailedGames, 1)
			return
		default:
			msg, err := lt.readMessage(p1Conn)
			if err != nil {
				continue
			}
			if msg.Type == "MATCH_FOUND" {
				var payload struct {
					GameID string `json:"game_id"`
				}
				json.Unmarshal(msg.Payload, &payload)
				gameID = payload.GameID
			}
		}
	}

	var gameWg sync.WaitGroup
	gameWg.Add(2)

	go lt.playAsPlayer(p1Conn, p1ID, gameID, &gameWg)
	go lt.playAsPlayer(p2Conn, p2ID, gameID, &gameWg)

	gameWg.Wait()
	atomic.AddInt64(&lt.results.CompletedGames, 1)
}

func (lt *LoadTester) playAsPlayer(conn *websocket.Conn, playerID, gameID string, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		msg, err := lt.readMessage(conn)
		if err != nil {
			return
		}

		if msg.Type == "GAME_SNAPSHOT" {
			var snapshot struct {
				State    string `json:"state"`
				Round    int    `json:"round"`
				Question *struct {
					Expression string `json:"expression"`
				} `json:"question"`
			}
			json.Unmarshal(msg.Payload, &snapshot)

			if snapshot.State == "game_over" || snapshot.State == "abandoned" {
				return
			}

			if snapshot.State == "round_active" && snapshot.Question != nil {
				if rand.Float64() < lt.config.ReconnectChance {
					conn.Close()
					time.Sleep(500 * time.Millisecond)
					newConn, err := lt.connect(playerID)
					if err == nil {
						conn = newConn
						lt.sendMessage(conn, "RECONNECT", map[string]string{
							"player_id": playerID,
							"game_id":   gameID,
						})
						atomic.AddInt64(&lt.results.Reconnections, 1)
					}
					continue
				}

				time.Sleep(lt.config.AnswerDelay + time.Duration(rand.Intn(500))*time.Millisecond)

				startTime := time.Now()
				answer := rand.Intn(200)
				lt.sendMessage(conn, "ANSWER", map[string]interface{}{
					"game_id":   gameID,
					"round":     snapshot.Round,
					"answer":    answer,
					"client_ts": time.Now().UnixMilli(),
				})
				atomic.AddInt64(&lt.results.TotalAnswers, 1)

				for {
					ackMsg, err := lt.readMessage(conn)
					if err != nil {
						break
					}
					if ackMsg.Type == "ANSWER_ACK" {
						latency := float64(time.Since(startTime).Milliseconds())
						lt.recordLatency(latency)

						var ack struct {
							Accepted bool `json:"accepted"`
						}
						json.Unmarshal(ackMsg.Payload, &ack)
						if ack.Accepted {
							atomic.AddInt64(&lt.results.AcceptedAnswers, 1)
						} else {
							atomic.AddInt64(&lt.results.RejectedAnswers, 1)
						}
						break
					}
					if ackMsg.Type == "GAME_SNAPSHOT" {
						break
					}
				}
			}
		}
	}
}

func (lt *LoadTester) connect(playerID string) (*websocket.Conn, error) {
	url := fmt.Sprintf("%s?player_id=%s&display_name=LoadTest", lt.config.ServerURL, playerID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	return conn, err
}

func (lt *LoadTester) sendMessage(conn *websocket.Conn, msgType string, payload interface{}) error {
	payloadBytes, _ := json.Marshal(payload)
	msg := Message{
		Type:    msgType,
		Payload: payloadBytes,
		TS:      time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(msg)
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (lt *LoadTester) readMessage(conn *websocket.Conn) (*Message, error) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var msg Message
	err = json.Unmarshal(data, &msg)
	return &msg, err
}

func (lt *LoadTester) recordLatency(latencyMs float64) {
	lt.mu.Lock()
	lt.latencies = append(lt.latencies, latencyMs)
	if latencyMs > lt.results.MaxLatencyMs {
		lt.results.MaxLatencyMs = latencyMs
	}
	lt.mu.Unlock()
}

func (lt *LoadTester) calculateLatencyStats() {
	if len(lt.latencies) == 0 {
		return
	}

	sorted := make([]float64, len(lt.latencies))
	copy(sorted, lt.latencies)

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var sum float64
	for _, l := range sorted {
		sum += l
	}
	lt.results.AvgLatencyMs = sum / float64(len(sorted))

	p99Index := int(float64(len(sorted)) * 0.99)
	if p99Index >= len(sorted) {
		p99Index = len(sorted) - 1
	}
	lt.results.P99LatencyMs = sorted[p99Index]
}

func (r *LoadTestResult) Print() {
	fmt.Println("\n========== LOAD TEST RESULTS ==========")
	fmt.Printf("Duration:          %v\n", r.Duration)
	fmt.Printf("Total Games:       %d\n", r.TotalGames)
	fmt.Printf("Completed Games:   %d (%.1f%%)\n", r.CompletedGames, 100*float64(r.CompletedGames)/float64(r.TotalGames))
	fmt.Printf("Failed Games:      %d\n", r.FailedGames)
	fmt.Printf("Total Answers:     %d\n", r.TotalAnswers)
	fmt.Printf("Accepted Answers:  %d (%.1f%%)\n", r.AcceptedAnswers, 100*float64(r.AcceptedAnswers)/float64(r.TotalAnswers))
	fmt.Printf("Rejected Answers:  %d\n", r.RejectedAnswers)
	fmt.Printf("Reconnections:     %d\n", r.Reconnections)
	fmt.Printf("Avg Latency:       %.2f ms\n", r.AvgLatencyMs)
	fmt.Printf("P99 Latency:       %.2f ms\n", r.P99LatencyMs)
	fmt.Printf("Max Latency:       %.2f ms\n", r.MaxLatencyMs)
	fmt.Printf("Errors:            %d\n", r.ErrorCount)
	fmt.Printf("Games/Second:      %.2f\n", float64(r.CompletedGames)/r.Duration.Seconds())
	fmt.Println("========================================")
}
