package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mentalarena/backend/internal/redis"
	"github.com/rs/zerolog"
)

type NodeInfo struct {
	NodeID    string
	Address   string
	StartedAt time.Time
	LastPing  time.Time
}

type ClusterManager struct {
	mu sync.RWMutex

	nodeID string
	redis  *redis.Client
	logger zerolog.Logger

	sessions map[string]bool

	stopCh chan struct{}
}

type ClusterConfig struct {
	NodeID       string
	RedisClient  *redis.Client
	Logger       zerolog.Logger
	HeartbeatTTL time.Duration
}

func NewClusterManager(cfg ClusterConfig) *ClusterManager {
	cm := &ClusterManager{
		nodeID:   cfg.NodeID,
		redis:    cfg.RedisClient,
		logger:   cfg.Logger.With().Str("component", "cluster").Str("node_id", cfg.NodeID).Logger(),
		sessions: make(map[string]bool),
		stopCh:   make(chan struct{}),
	}

	go cm.heartbeatLoop(cfg.HeartbeatTTL)
	return cm
}

func (cm *ClusterManager) RegisterNode(address string) error {
	key := fmt.Sprintf("cluster:node:%s", cm.nodeID)
	data := fmt.Sprintf("%s|%d", address, time.Now().Unix())
	return cm.redis.Set(key, data, 30*time.Second)
}

func (cm *ClusterManager) AcquireSession(gameID string) (bool, error) {
	key := fmt.Sprintf("session:owner:%s", gameID)

	acquired, err := cm.redis.SetNX(key, cm.nodeID, 5*time.Minute)
	if err != nil {
		return false, err
	}

	if acquired {
		cm.mu.Lock()
		cm.sessions[gameID] = true
		cm.mu.Unlock()
		cm.logger.Info().Str("game_id", gameID).Msg("acquired_session_ownership")
	}

	return acquired, nil
}

func (cm *ClusterManager) ReleaseSession(gameID string) error {
	key := fmt.Sprintf("session:owner:%s", gameID)

	cm.mu.Lock()
	delete(cm.sessions, gameID)
	cm.mu.Unlock()

	return cm.redis.Del(key)
}

func (cm *ClusterManager) GetSessionOwner(gameID string) (string, error) {
	key := fmt.Sprintf("session:owner:%s", gameID)
	return cm.redis.Get(key)
}

func (cm *ClusterManager) IsSessionOwner(gameID string) bool {
	cm.mu.RLock()
	owned := cm.sessions[gameID]
	cm.mu.RUnlock()
	return owned
}

func (cm *ClusterManager) RefreshSessionOwnership() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for gameID := range cm.sessions {
		key := fmt.Sprintf("session:owner:%s", gameID)
		if err := cm.redis.Expire(key, 5*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

func (cm *ClusterManager) heartbeatLoop(ttl time.Duration) {
	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			key := fmt.Sprintf("cluster:node:%s", cm.nodeID)
			cm.redis.Expire(key, ttl)

			cm.RefreshSessionOwnership()

		case <-cm.stopCh:
			return
		}
	}
}

func (cm *ClusterManager) Stop() {
	close(cm.stopCh)

	cm.mu.Lock()
	for gameID := range cm.sessions {
		key := fmt.Sprintf("session:owner:%s", gameID)
		cm.redis.Del(key)
	}
	cm.sessions = make(map[string]bool)
	cm.mu.Unlock()

	key := fmt.Sprintf("cluster:node:%s", cm.nodeID)
	cm.redis.Del(key)

	cm.logger.Info().Msg("cluster_manager_stopped")
}

type ConsistentHash struct {
	nodes    []string
	replicas int
}

func NewConsistentHash(nodes []string, replicas int) *ConsistentHash {
	return &ConsistentHash{
		nodes:    nodes,
		replicas: replicas,
	}
}

func (ch *ConsistentHash) GetNode(gameID string) string {
	if len(ch.nodes) == 0 {
		return ""
	}
	hash := uint32(0)
	for _, c := range gameID {
		hash = hash*31 + uint32(c)
	}
	return ch.nodes[hash%uint32(len(ch.nodes))]
}

type SessionRouter struct {
	cluster *ClusterManager
	redis   *redis.Client
	logger  zerolog.Logger
}

func NewSessionRouter(cluster *ClusterManager, r *redis.Client, logger zerolog.Logger) *SessionRouter {
	return &SessionRouter{
		cluster: cluster,
		redis:   r,
		logger:  logger.With().Str("component", "router").Logger(),
	}
}

func (sr *SessionRouter) RouteToPlayer(playerID, nodeID string) error {
	key := fmt.Sprintf("route:player:%s", playerID)
	return sr.redis.Set(key, nodeID, 30*time.Minute)
}

func (sr *SessionRouter) GetPlayerNode(playerID string) (string, error) {
	key := fmt.Sprintf("route:player:%s", playerID)
	return sr.redis.Get(key)
}

func (sr *SessionRouter) RouteToGame(gameID, nodeID string) error {
	key := fmt.Sprintf("route:game:%s", gameID)
	return sr.redis.Set(key, nodeID, 1*time.Hour)
}

func (sr *SessionRouter) GetGameNode(gameID string) (string, error) {
	key := fmt.Sprintf("route:game:%s", gameID)
	return sr.redis.Get(key)
}

type ClusterHealth struct {
	nodeID string
	redis  *redis.Client
	logger zerolog.Logger
}

func NewClusterHealth(nodeID string, r *redis.Client, logger zerolog.Logger) *ClusterHealth {
	return &ClusterHealth{
		nodeID: nodeID,
		redis:  r,
		logger: logger,
	}
}

func (ch *ClusterHealth) CheckHealth(ctx context.Context) error {
	if _, err := ch.redis.Get("health:ping"); err != nil && err.Error() != "redis: nil" {
		return fmt.Errorf("redis health check failed: %w", err)
	}
	return nil
}

func (ch *ClusterHealth) GetActiveNodes() ([]NodeInfo, error) {

	return []NodeInfo{
		{NodeID: ch.nodeID, StartedAt: time.Now(), LastPing: time.Now()},
	}, nil
}
