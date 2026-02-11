package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerPort string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret string

	TotalRounds      int
	RoundDuration    time.Duration
	CountdownSeconds int
	GracePeriod      time.Duration

	MaxMessagesPerSecond  int
	MaxQueueActionsPerMin int
}

func Load() *Config {
	return &Config{
		ServerPort:            getEnv("SERVER_PORT", "8080"),
		RedisAddr:             getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:         getEnv("REDIS_PASSWORD", ""),
		RedisDB:               getEnvInt("REDIS_DB", 0),
		JWTSecret:             getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		TotalRounds:           getEnvInt("TOTAL_ROUNDS", 10),
		RoundDuration:         time.Duration(getEnvInt("ROUND_DURATION_SEC", 10)) * time.Second,
		CountdownSeconds:      getEnvInt("COUNTDOWN_SECONDS", 3),
		GracePeriod:           time.Duration(getEnvInt("GRACE_PERIOD_SEC", 30)) * time.Second,
		MaxMessagesPerSecond:  getEnvInt("MAX_MESSAGES_PER_SEC", 20),
		MaxQueueActionsPerMin: getEnvInt("MAX_QUEUE_ACTIONS_PER_MIN", 5),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
