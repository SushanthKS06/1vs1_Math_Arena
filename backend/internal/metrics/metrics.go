package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MatchesCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "matharena_matches_created_total",
		Help: "Total number of matches created",
	})

	MatchesCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_matches_completed_total",
		Help: "Total number of matches completed by outcome",
	}, []string{"outcome"})

	ActiveGames = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "matharena_active_games",
		Help: "Number of currently active games",
	})

	RoundsPlayed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "matharena_rounds_played_total",
		Help: "Total number of rounds played",
	})

	RoundDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "matharena_round_duration_seconds",
		Help:    "Duration of rounds in seconds",
		Buckets: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20},
	})

	AnswersSubmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_answers_submitted_total",
		Help: "Total number of answers submitted",
	}, []string{"result"})

	AnswerLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "matharena_answer_latency_ms",
		Help:    "Latency of answer submissions in milliseconds",
		Buckets: []float64{10, 50, 100, 200, 500, 1000, 2000, 5000},
	})

	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "matharena_active_connections",
		Help: "Number of currently active WebSocket connections",
	})

	ConnectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "matharena_connections_total",
		Help: "Total number of WebSocket connections established",
	})

	DisconnectsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_disconnects_total",
		Help: "Total number of disconnections by reason",
	}, []string{"reason"})

	Reconnections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "matharena_reconnections_total",
		Help: "Total number of successful reconnections",
	})

	QueueLength = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "matharena_queue_length",
		Help: "Current length of the matchmaking queue",
	})

	QueueWaitTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "matharena_queue_wait_seconds",
		Help:    "Time spent waiting in matchmaking queue",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	})

	MatchmakingFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "matharena_matchmaking_failures_total",
		Help: "Total number of matchmaking failures",
	})

	DesyncEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_desync_events_total",
		Help: "Total number of potential desynchronization events detected",
	}, []string{"type"})

	MessagesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_messages_received_total",
		Help: "Total number of WebSocket messages received by type",
	}, []string{"type"})

	MessagesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_messages_sent_total",
		Help: "Total number of WebSocket messages sent by type",
	}, []string{"type"})

	Errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "matharena_errors_total",
		Help: "Total number of errors by type",
	}, []string{"component", "type"})
)

func RecordMatchCompleted(outcome string) {
	MatchesCompleted.WithLabelValues(outcome).Inc()
	ActiveGames.Dec()
}

func RecordAnswer(result string, latencyMs float64) {
	AnswersSubmitted.WithLabelValues(result).Inc()
	AnswerLatency.Observe(latencyMs)
}

func RecordDesync(eventType string) {
	DesyncEvents.WithLabelValues(eventType).Inc()
}

func RecordError(component, errorType string) {
	Errors.WithLabelValues(component, errorType).Inc()
}
