package ws

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/mentalarena/backend/internal/metrics"
	"github.com/mentalarena/backend/internal/security"
	"github.com/rs/zerolog"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Handler struct {
	hub           *Hub
	jwtSecret     []byte
	logger        zerolog.Logger
	abuseDetector *security.IPAbuseDetector
	rateLimiter   *security.RateLimiter
}

func NewHandler(hub *Hub, jwtSecret string, logger zerolog.Logger) *Handler {
	return &Handler{
		hub:           hub,
		jwtSecret:     []byte(jwtSecret),
		logger:        logger.With().Str("component", "ws_handler").Logger(),
		abuseDetector: security.NewIPAbuseDetector(50, 1*time.Minute),
		rateLimiter:   security.NewRateLimiter(10, 1*time.Second),
	}
}

func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return r.RemoteAddr
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientIP := getClientIP(r)

	if !h.abuseDetector.RecordConnection(clientIP) {
		h.logger.Warn().Str("ip", clientIP).Msg("ip_blocked_abuse")
		metrics.RecordError("ws_handler", "ip_blocked")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	playerID, displayName, err := h.authenticate(r)
	if err != nil {
		h.logger.Warn().Err(err).Str("ip", clientIP).Msg("authentication_failed")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if !h.rateLimiter.Allow(playerID) {
		h.logger.Warn().Str("player_id", playerID).Msg("rate_limited")
		metrics.RecordError("ws_handler", "rate_limited")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error().Err(err).Msg("websocket_upgrade_failed")
		return
	}

	metrics.ConnectionsTotal.Inc()
	metrics.ActiveConnections.Inc()

	client := NewClient(h.hub, conn, playerID, displayName, h.logger)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

func (h *Handler) authenticate(r *http.Request) (string, string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		return h.parseJWT(tokenString)
	}

	tokenString := r.URL.Query().Get("token")
	if tokenString != "" {
		return h.parseJWT(tokenString)
	}

	playerID := r.URL.Query().Get("player_id")
	displayName := r.URL.Query().Get("display_name")
	if playerID != "" {
		if displayName == "" {
			displayName = "Player_" + playerID[:8]
		}
		return playerID, displayName, nil
	}

	return "", "", ErrMissingToken
}

func (h *Handler) parseJWT(tokenString string) (string, string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return h.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return "", "", ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", ErrInvalidToken
	}

	playerID, ok := claims["sub"].(string)
	if !ok || playerID == "" {
		return "", "", ErrInvalidToken
	}

	displayName, _ := claims["name"].(string)
	if displayName == "" {
		displayName = "Player"
	}

	return playerID, displayName, nil
}

var (
	ErrMissingToken = &AuthError{Code: "missing_token", Message: "Missing authentication token"}
	ErrInvalidToken = &AuthError{Code: "invalid_token", Message: "Invalid authentication token"}
)

type AuthError struct {
	Code    string
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}
