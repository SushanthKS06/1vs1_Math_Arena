package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/mentalarena/backend/tests/loadtest"
)

func main() {
	serverURL := flag.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	numGames := flag.Int("games", 100, "Number of concurrent games to simulate")
	rampUp := flag.Duration("ramp", 10*time.Second, "Ramp-up duration")
	testDuration := flag.Duration("duration", 60*time.Second, "Test duration")
	answerDelay := flag.Duration("delay", 500*time.Millisecond, "Answer delay (think time)")
	reconnectChance := flag.Float64("reconnect", 0.05, "Reconnect probability (0-1)")

	flag.Parse()

	fmt.Println("Mental Math Arena - Load Test")
	fmt.Println("==============================")
	fmt.Printf("Server:           %s\n", *serverURL)
	fmt.Printf("Games:            %d\n", *numGames)
	fmt.Printf("Ramp-up:          %v\n", *rampUp)
	fmt.Printf("Duration:         %v\n", *testDuration)
	fmt.Printf("Answer Delay:     %v\n", *answerDelay)
	fmt.Printf("Reconnect Chance: %.1f%%\n", *reconnectChance*100)

	tester := loadtest.NewLoadTester(loadtest.LoadTestConfig{
		ServerURL:       *serverURL,
		NumGames:        *numGames,
		RampUpDuration:  *rampUp,
		TestDuration:    *testDuration,
		AnswerDelay:     *answerDelay,
		ReconnectChance: *reconnectChance,
	})

	result, err := tester.Run()
	if err != nil {
		fmt.Printf("Load test failed: %v\n", err)
		return
	}

	result.Print()

	passRate := float64(result.CompletedGames) / float64(result.TotalGames)
	if passRate >= 0.95 && result.P99LatencyMs < 1000 {
		fmt.Println("\n✅ LOAD TEST PASSED")
	} else {
		fmt.Println("\n❌ LOAD TEST FAILED")
		if passRate < 0.95 {
			fmt.Printf("   - Completion rate %.1f%% < 95%%\n", passRate*100)
		}
		if result.P99LatencyMs >= 1000 {
			fmt.Printf("   - P99 latency %.0fms >= 1000ms\n", result.P99LatencyMs)
		}
	}
}
