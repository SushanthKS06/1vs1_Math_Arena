package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mentalarena/backend/internal/config"
	"github.com/mentalarena/backend/internal/game"
	"github.com/mentalarena/backend/internal/matchmaker"
	redisclient "github.com/mentalarena/backend/internal/redis"
	"github.com/mentalarena/backend/internal/ws"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Str("service", "math-arena").
		Logger()

	cfg := config.Load()
	logger.Info().
		Str("port", cfg.ServerPort).
		Str("redis", cfg.RedisAddr).
		Msg("starting server")

	redis, err := redisclient.NewClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to redis")
	}
	defer redis.Close()
	logger.Info().Msg("connected to redis")

	gameManager := game.NewGameManager(game.ManagerConfig{
		TotalRounds:      cfg.TotalRounds,
		RoundDuration:    cfg.RoundDuration,
		GracePeriod:      cfg.GracePeriod,
		CountdownSeconds: cfg.CountdownSeconds,
		Difficulty:       2,
	}, redis, logger)

	mm := matchmaker.NewMatchmaker(redis, gameManager, logger)

	hub := ws.NewHub(gameManager, mm, redis, logger)
	go hub.Run()

	mm.SetOnMatchFound(hub.SendToPlayer)

	mm.Start()
	defer mm.Stop()

	wsHandler := ws.NewHandler(hub, cfg.JWTSecret, logger)

	mux := http.NewServeMux()

	mux.Handle("/ws", wsHandler)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"status":"healthy","active_games":%d,"queue_length":%d}`,
			gameManager.ActiveGameCount(), mm.QueueLength())))
	})

	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info().Str("addr", server.Addr).Msg("server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("shutting down server")

	shutdownTimeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	mm.Stop()

	gameShutdownCtx, gameCancel := context.WithTimeout(ctx, 20*time.Second)
	defer gameCancel()

	if err := gameManager.GracefulShutdown(gameShutdownCtx); err != nil {
		logger.Warn().Err(err).Msg("game shutdown timeout, some games may be interrupted")
	}

	if err := server.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("server shutdown error")
	} else {
		logger.Info().Msg("server shutdown complete")
	}
}
