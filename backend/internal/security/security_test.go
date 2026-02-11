package security

import (
	"testing"
	"time"
)

func TestAnswerSigner(t *testing.T) {
	signer := NewAnswerSigner("test-secret-key")

	gameID := "game-123"
	round := 5
	answer := 42
	timestamp := time.Now().UnixMilli()
	nonce := "unique-nonce-123"

	signature := signer.SignAnswer(gameID, round, answer, timestamp, nonce)

	if !signer.VerifyAnswer(gameID, round, answer, timestamp, nonce, signature) {
		t.Error("valid signature should verify")
	}

	if signer.VerifyAnswer(gameID, round, answer+1, timestamp, nonce, signature) {
		t.Error("tampered answer should fail verification")
	}

	if signer.VerifyAnswer(gameID, round+1, answer, timestamp, nonce, signature) {
		t.Error("tampered round should fail verification")
	}

	if signer.VerifyAnswer("different-game", round, answer, timestamp, nonce, signature) {
		t.Error("tampered game_id should fail verification")
	}
}

func TestAnswerSignerTimestamp(t *testing.T) {
	signer := NewAnswerSigner("test-secret")

	if !signer.VerifyTimestamp(time.Now().UnixMilli(), 30*time.Second) {
		t.Error("current timestamp should be valid")
	}

	old := time.Now().Add(-1 * time.Minute).UnixMilli()
	if signer.VerifyTimestamp(old, 30*time.Second) {
		t.Error("old timestamp should be invalid")
	}

	future := time.Now().Add(2 * time.Second).UnixMilli()
	if !signer.VerifyTimestamp(future, 30*time.Second) {
		t.Error("slight future timestamp should be allowed")
	}

	tooFuture := time.Now().Add(10 * time.Second).UnixMilli()
	if signer.VerifyTimestamp(tooFuture, 30*time.Second) {
		t.Error("far future timestamp should be invalid")
	}
}

func TestNonceStore(t *testing.T) {
	store := NewNonceStore(1 * time.Minute)

	nonce := "test-nonce-1"

	if store.IsUsed(nonce) {
		t.Error("first use of nonce should not be marked as used")
	}

	if !store.IsUsed(nonce) {
		t.Error("second use of nonce should be marked as used (replay)")
	}

	if store.IsUsed("different-nonce") {
		t.Error("different nonce should not be marked as used")
	}
}

func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(3, 100*time.Millisecond)

	key := "player-1"

	for i := 0; i < 3; i++ {
		if !limiter.Allow(key) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	if limiter.Allow(key) {
		t.Error("4th request should be rate limited")
	}

	time.Sleep(150 * time.Millisecond)

	if !limiter.Allow(key) {
		t.Error("request after window should be allowed")
	}

	if !limiter.Allow("player-2") {
		t.Error("different key should have its own rate limit")
	}
}

func TestIPAbuseDetector(t *testing.T) {
	detector := NewIPAbuseDetector(5, 100*time.Millisecond)

	ip := "192.168.1.1"

	for i := 0; i < 5; i++ {
		if !detector.RecordConnection(ip) {
			t.Errorf("connection %d should be allowed", i+1)
		}
	}

	if detector.RecordConnection(ip) {
		t.Error("6th connection should be blocked")
	}

	time.Sleep(150 * time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	if !detector.RecordConnection("192.168.1.2") {
		t.Error("different IP should be allowed")
	}
}

func TestNonceValidation(t *testing.T) {
	if !ValidateNonceFormat("0123456789abcdef0123456789abcdef") {
		t.Error("valid nonce should pass validation")
	}

	if ValidateNonceFormat("0123456789abcdef") {
		t.Error("short nonce should fail validation")
	}

	if ValidateNonceFormat("0123456789abcdef0123456789abcdef00") {
		t.Error("long nonce should fail validation")
	}

	if ValidateNonceFormat("0123456789abcdefGHIJKLMN01234567") {
		t.Error("non-hex nonce should fail validation")
	}
}

func TestGenerateNonce(t *testing.T) {
	nonce1 := GenerateNonce("game-1", "player-1", 1)
	time.Sleep(1 * time.Nanosecond)
	nonce2 := GenerateNonce("game-1", "player-1", 1)

	if nonce1 == nonce2 {
		t.Error("generated nonces should be unique")
	}

	if !ValidateNonceFormat(nonce1) {
		t.Error("generated nonce should have valid format")
	}
}
