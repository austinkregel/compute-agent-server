package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

var (
	ErrMissingParams   = errors.New("missing required auth params (clientId, ts, sig)")
	ErrInvalidClientID = errors.New("clientId exceeds maximum length")
	ErrBadTimestamp    = errors.New("invalid or missing timestamp")
	ErrStaleTimestamp  = errors.New("timestamp outside allowed skew window")
	ErrBadSignature    = errors.New("invalid HMAC signature")
)

// MaxClientIDLength is the maximum allowed clientId length.
const MaxClientIDLength = 128

// AgentHandshake contains the parsed query parameters from an agent connection.
type AgentHandshake struct {
	ClientID string
	Ts       int64  // milliseconds
	Sig      string // hex-encoded HMAC-SHA256
}

// ValidateAgentHandshake verifies an agent's HMAC authentication.
// authToken is the shared secret, maxSkew is the allowed clock difference.
func ValidateAgentHandshake(h AgentHandshake, authToken string, maxSkew time.Duration) error {
	if h.ClientID == "" || h.Sig == "" {
		return ErrMissingParams
	}
	if len(h.ClientID) > MaxClientIDLength {
		return ErrInvalidClientID
	}
	if h.Ts <= 0 {
		return ErrBadTimestamp
	}

	// Check timestamp freshness
	now := time.Now().UnixMilli()
	skewMs := maxSkew.Milliseconds()
	diff := now - h.Ts
	if diff < 0 {
		diff = -diff
	}
	if diff > skewMs {
		return ErrStaleTimestamp
	}

	// Compute expected signature: HMAC-SHA256(authToken, JSON.stringify({clientId, ts}))
	payload := map[string]any{
		"clientId": h.ClientID,
		"ts":       h.Ts,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write(payloadJSON)
	expected := mac.Sum(nil)

	// Decode provided signature
	sigBytes, err := hex.DecodeString(h.Sig)
	if err != nil {
		return ErrBadSignature
	}

	// Constant-time comparison
	if subtle.ConstantTimeCompare(expected, sigBytes) != 1 {
		return ErrBadSignature
	}

	return nil
}

// ParseHandshakeParams extracts handshake params from query string values.
func ParseHandshakeParams(clientID, tsStr, sig string) (AgentHandshake, error) {
	if clientID == "" || tsStr == "" || sig == "" {
		return AgentHandshake{}, ErrMissingParams
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || ts <= 0 || ts > math.MaxInt64/2 {
		return AgentHandshake{}, ErrBadTimestamp
	}

	return AgentHandshake{
		ClientID: clientID,
		Ts:       ts,
		Sig:      sig,
	}, nil
}
