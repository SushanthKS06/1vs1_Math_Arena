package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

type AnswerSigner struct {
	secret []byte
}

func NewAnswerSigner(secret string) *AnswerSigner {
	return &AnswerSigner{
		secret: []byte(secret),
	}
}

func (s *AnswerSigner) SignAnswer(gameID string, round, answer int, timestamp int64, nonce string) string {
	data := fmt.Sprintf("%s|%d|%d|%d|%s", gameID, round, answer, timestamp, nonce)
	return s.sign(data)
}

func (s *AnswerSigner) VerifyAnswer(gameID string, round, answer int, timestamp int64, nonce, signature string) bool {
	expected := s.SignAnswer(gameID, round, answer, timestamp, nonce)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (s *AnswerSigner) VerifyTimestamp(timestamp int64, maxAge time.Duration) bool {
	now := time.Now().UnixMilli()
	age := time.Duration(now-timestamp) * time.Millisecond

	if age < -5*time.Second {
		return false
	}
	if age > maxAge {
		return false
	}
	return true
}

func (s *AnswerSigner) sign(data string) string {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

type NonceStore struct {
	nonces map[string]int64
	maxAge time.Duration
}

func NewNonceStore(maxAge time.Duration) *NonceStore {
	store := &NonceStore{
		nonces: make(map[string]int64),
		maxAge: maxAge,
	}
	go store.cleanup()
	return store
}

func (ns *NonceStore) IsUsed(nonce string) bool {
	if _, exists := ns.nonces[nonce]; exists {
		return true
	}
	ns.nonces[nonce] = time.Now().UnixMilli()
	return false
}

func (ns *NonceStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		cutoff := time.Now().Add(-ns.maxAge).UnixMilli()
		for nonce, ts := range ns.nonces {
			if ts < cutoff {
				delete(ns.nonces, nonce)
			}
		}
	}
}

type RateLimiter struct {
	windows map[string]*slidingWindow
	limit   int
	window  time.Duration
}

type slidingWindow struct {
	timestamps []int64
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		windows: make(map[string]*slidingWindow),
		limit:   limit,
		window:  window,
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	now := time.Now().UnixMilli()
	cutoff := now - rl.window.Milliseconds()

	sw, exists := rl.windows[key]
	if !exists {
		sw = &slidingWindow{timestamps: make([]int64, 0, rl.limit)}
		rl.windows[key] = sw
	}

	valid := make([]int64, 0, len(sw.timestamps))
	for _, ts := range sw.timestamps {
		if ts > cutoff {
			valid = append(valid, ts)
		}
	}
	sw.timestamps = valid

	if len(sw.timestamps) >= rl.limit {
		return false
	}

	sw.timestamps = append(sw.timestamps, now)
	return true
}

type IPAbuseDetector struct {
	connections map[string]*ipStats
	threshold   int
	window      time.Duration
}

type ipStats struct {
	connections []int64
	blocked     bool
	blockedAt   int64
}

func NewIPAbuseDetector(threshold int, window time.Duration) *IPAbuseDetector {
	return &IPAbuseDetector{
		connections: make(map[string]*ipStats),
		threshold:   threshold,
		window:      window,
	}
}

func (d *IPAbuseDetector) RecordConnection(ip string) bool {
	now := time.Now().UnixMilli()
	cutoff := now - d.window.Milliseconds()

	stats, exists := d.connections[ip]
	if !exists {
		stats = &ipStats{connections: make([]int64, 0)}
		d.connections[ip] = stats
	}

	if stats.blocked {
		if now-stats.blockedAt > d.window.Milliseconds() {
			stats.blocked = false
		} else {
			return false
		}
	}

	valid := make([]int64, 0, len(stats.connections))
	for _, ts := range stats.connections {
		if ts > cutoff {
			valid = append(valid, ts)
		}
	}
	stats.connections = valid

	if len(stats.connections) >= d.threshold {
		stats.blocked = true
		stats.blockedAt = now
		return false
	}

	stats.connections = append(stats.connections, now)
	return true
}

func GenerateNonce(gameID string, playerID string, round int) string {
	data := fmt.Sprintf("%s:%s:%d:%d", gameID, playerID, round, time.Now().UnixNano())
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:16])
}

func ValidateNonceFormat(nonce string) bool {
	if len(nonce) != 32 {
		return false
	}
	_, err := hex.DecodeString(nonce)
	return err == nil
}

func ParseInt(s string, defaultVal int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return defaultVal
}
