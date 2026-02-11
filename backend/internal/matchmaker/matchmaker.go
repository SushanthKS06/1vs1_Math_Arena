package matchmaker

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/mentalarena/backend/internal/game"
	"github.com/mentalarena/backend/internal/protocol"
	redisclient "github.com/mentalarena/backend/internal/redis"
	"github.com/rs/zerolog"
)

const (
	queueKey        = "mm:queue"
	playerKeyPrefix = "mm:queue:"
	lockKeyPrefix   = "lock:match:"

	playerTTL = 5 * time.Minute
	lockTTL   = 10 * time.Second
)

type QueuedPlayer struct {
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	QueuedAt    int64  `json:"queued_at"`
}

type Match struct {
	GameID  string
	Player1 QueuedPlayer
	Player2 QueuedPlayer
}

type Matchmaker struct {
	redis       *redisclient.Client
	gameManager *game.GameManager

	onMatchFound func(playerID string, msg *protocol.Message)

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}

	logger zerolog.Logger
}

func NewMatchmaker(redis *redisclient.Client, gm *game.GameManager, logger zerolog.Logger) *Matchmaker {
	return &Matchmaker{
		redis:       redis,
		gameManager: gm,
		stopCh:      make(chan struct{}),
		logger:      logger.With().Str("component", "matchmaker").Logger(),
	}
}

func (m *Matchmaker) SetOnMatchFound(fn func(playerID string, msg *protocol.Message)) {
	m.onMatchFound = fn
}

func (m *Matchmaker) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go m.matchLoop()
}

func (m *Matchmaker) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopCh)
}

func (m *Matchmaker) EnqueuePlayer(player QueuedPlayer) (int, error) {
	locked, err := m.redis.SetNX(lockKeyPrefix+player.PlayerID, "1", lockTTL)
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, ErrAlreadyQueued
	}

	player.QueuedAt = time.Now().UnixMilli()
	data, err := json.Marshal(player)
	if err != nil {
		m.redis.Del(lockKeyPrefix + player.PlayerID)
		return 0, err
	}

	if err := m.redis.Set(playerKeyPrefix+player.PlayerID, data, playerTTL); err != nil {
		m.redis.Del(lockKeyPrefix + player.PlayerID)
		return 0, err
	}

	if err := m.redis.RPush(queueKey, player.PlayerID); err != nil {
		m.redis.Del(lockKeyPrefix+player.PlayerID, playerKeyPrefix+player.PlayerID)
		return 0, err
	}

	qLen, _ := m.redis.LLen(queueKey)

	m.logger.Info().
		Str("player_id", player.PlayerID).
		Int64("position", qLen).
		Msg("player_enqueued")

	return int(qLen), nil
}

func (m *Matchmaker) DequeuePlayer(playerID string) error {
	if err := m.redis.LRem(queueKey, 1, playerID); err != nil {
		return err
	}

	m.redis.Del(lockKeyPrefix+playerID, playerKeyPrefix+playerID)

	m.logger.Info().Str("player_id", playerID).Msg("player_dequeued")
	return nil
}

func (m *Matchmaker) matchLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.tryMatch()
		}
	}
}

func (m *Matchmaker) tryMatch() {
	script := `
		local qlen = redis.call('LLEN', KEYS[1])
		if qlen < 2 then
			return nil
		end
		local p1 = redis.call('LPOP', KEYS[1])
		local p2 = redis.call('LPOP', KEYS[1])
		if p1 and p2 then
			return {p1, p2}
		elseif p1 then
			redis.call('RPUSH', KEYS[1], p1)
		end
		return nil
	`

	result, err := m.redis.Underlying().Eval(m.redis.Context(), script, []string{queueKey}).Result()
	if err != nil || result == nil {
		return
	}

	players, ok := result.([]interface{})
	if !ok || len(players) != 2 {
		return
	}

	p1ID, ok1 := players[0].(string)
	p2ID, ok2 := players[1].(string)
	if !ok1 || !ok2 {
		return
	}

	p1, err := m.getPlayerDetails(p1ID)
	if err != nil {
		m.redis.RPush(queueKey, p2ID)
		m.cleanupPlayerKeys(p1ID)
		return
	}

	p2, err := m.getPlayerDetails(p2ID)
	if err != nil {
		m.redis.RPush(queueKey, p1ID)
		m.cleanupPlayerKeys(p2ID)
		return
	}

	m.createMatch(p1, p2)
}

func (m *Matchmaker) cleanupPlayerKeys(playerID string) {
	m.redis.Del(lockKeyPrefix+playerID, playerKeyPrefix+playerID)
}

func (m *Matchmaker) getPlayerDetails(playerID string) (QueuedPlayer, error) {
	data, err := m.redis.Get(playerKeyPrefix + playerID)
	if err != nil {
		return QueuedPlayer{}, err
	}

	var player QueuedPlayer
	if err := json.Unmarshal([]byte(data), &player); err != nil {
		return QueuedPlayer{}, err
	}

	return player, nil
}

func (m *Matchmaker) createMatch(p1, p2 QueuedPlayer) {
	m.redis.Del(
		lockKeyPrefix+p1.PlayerID, playerKeyPrefix+p1.PlayerID,
		lockKeyPrefix+p2.PlayerID, playerKeyPrefix+p2.PlayerID,
	)

	session, err := m.gameManager.CreateGame(
		game.PlayerInfo{PlayerID: p1.PlayerID, DisplayName: p1.DisplayName},
		game.PlayerInfo{PlayerID: p2.PlayerID, DisplayName: p2.DisplayName},
	)
	if err != nil {
		m.logger.Error().Err(err).Msg("failed_to_create_game")
		return
	}

	startsAt := time.Now().Add(3 * time.Second).UnixMilli()

	m.notifyPlayer(p1.PlayerID, session.GameID, protocol.PlayerInfo{
		PlayerID:    p2.PlayerID,
		DisplayName: p2.DisplayName,
	}, startsAt)

	m.notifyPlayer(p2.PlayerID, session.GameID, protocol.PlayerInfo{
		PlayerID:    p1.PlayerID,
		DisplayName: p1.DisplayName,
	}, startsAt)

	m.gameManager.StartGame(session.GameID)

	m.logger.Info().
		Str("game_id", session.GameID).
		Str("player1", p1.PlayerID).
		Str("player2", p2.PlayerID).
		Msg("match_created")
}

func (m *Matchmaker) notifyPlayer(playerID, gameID string, opponent protocol.PlayerInfo, startsAt int64) {
	if m.onMatchFound == nil {
		return
	}

	msg := protocol.MustMessage(protocol.MsgMatchFound, protocol.MatchFoundPayload{
		GameID:   gameID,
		Opponent: opponent,
		StartsAt: startsAt,
	})

	m.onMatchFound(playerID, msg)
}

func (m *Matchmaker) QueueLength() int64 {
	length, _ := m.redis.LLen(queueKey)
	return length
}

var (
	ErrAlreadyQueued = &MatchmakerError{Code: "already_queued", Message: "Player already in queue"}
)

type MatchmakerError struct {
	Code    string
	Message string
}

func (e *MatchmakerError) Error() string {
	return e.Message
}
