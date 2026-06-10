package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func computeTestSig(clientID string, ts int64, authToken string) string {
	payload := map[string]any{"clientId": clientID, "ts": ts}
	payloadJSON, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write(payloadJSON)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestValidateAgentHandshake_Success(t *testing.T) {
	token := "test-secret-token"
	ts := time.Now().UnixMilli()
	sig := computeTestSig("node-1", ts, token)

	h := AgentHandshake{ClientID: "node-1", Ts: ts, Sig: sig}
	err := ValidateAgentHandshake(h, token, 10*time.Minute)
	if err != nil {
		t.Fatalf("ValidateAgentHandshake() error = %v", err)
	}
}

func TestValidateAgentHandshake_BadSignature(t *testing.T) {
	token := "test-secret-token"
	ts := time.Now().UnixMilli()
	sig := computeTestSig("node-1", ts, "wrong-token")

	h := AgentHandshake{ClientID: "node-1", Ts: ts, Sig: sig}
	err := ValidateAgentHandshake(h, token, 10*time.Minute)
	if err != ErrBadSignature {
		t.Errorf("error = %v, want ErrBadSignature", err)
	}
}

func TestValidateAgentHandshake_StaleTimestamp(t *testing.T) {
	token := "test-secret-token"
	ts := time.Now().Add(-20 * time.Minute).UnixMilli()
	sig := computeTestSig("node-1", ts, token)

	h := AgentHandshake{ClientID: "node-1", Ts: ts, Sig: sig}
	err := ValidateAgentHandshake(h, token, 10*time.Minute)
	if err != ErrStaleTimestamp {
		t.Errorf("error = %v, want ErrStaleTimestamp", err)
	}
}

func TestValidateAgentHandshake_FutureTimestamp(t *testing.T) {
	token := "test-secret-token"
	ts := time.Now().Add(20 * time.Minute).UnixMilli()
	sig := computeTestSig("node-1", ts, token)

	h := AgentHandshake{ClientID: "node-1", Ts: ts, Sig: sig}
	err := ValidateAgentHandshake(h, token, 10*time.Minute)
	if err != ErrStaleTimestamp {
		t.Errorf("error = %v, want ErrStaleTimestamp", err)
	}
}

func TestValidateAgentHandshake_MissingParams(t *testing.T) {
	h := AgentHandshake{}
	err := ValidateAgentHandshake(h, "token", 10*time.Minute)
	if err != ErrMissingParams {
		t.Errorf("error = %v, want ErrMissingParams", err)
	}
}

func TestValidateAgentHandshake_OversizedClientID(t *testing.T) {
	token := "test-secret-token"
	bigID := string(make([]byte, MaxClientIDLength+1))
	ts := time.Now().UnixMilli()
	sig := computeTestSig(bigID, ts, token)

	h := AgentHandshake{ClientID: bigID, Ts: ts, Sig: sig}
	err := ValidateAgentHandshake(h, token, 10*time.Minute)
	if err != ErrInvalidClientID {
		t.Errorf("error = %v, want ErrInvalidClientID", err)
	}
}

func TestValidateAgentHandshake_InvalidHexSig(t *testing.T) {
	h := AgentHandshake{ClientID: "node-1", Ts: time.Now().UnixMilli(), Sig: "not-hex"}
	err := ValidateAgentHandshake(h, "token", 10*time.Minute)
	if err != ErrBadSignature {
		t.Errorf("error = %v, want ErrBadSignature", err)
	}
}

func TestParseHandshakeParams(t *testing.T) {
	h, err := ParseHandshakeParams("node-1", "1700000000000", "abcdef")
	if err != nil {
		t.Fatalf("ParseHandshakeParams() error = %v", err)
	}
	if h.ClientID != "node-1" {
		t.Errorf("ClientID = %q", h.ClientID)
	}
	if h.Ts != 1700000000000 {
		t.Errorf("Ts = %d", h.Ts)
	}
}

func TestParseHandshakeParams_Missing(t *testing.T) {
	_, err := ParseHandshakeParams("", "123", "abc")
	if err != ErrMissingParams {
		t.Errorf("error = %v, want ErrMissingParams", err)
	}
}

func TestParseHandshakeParams_BadTs(t *testing.T) {
	_, err := ParseHandshakeParams("node-1", "notanumber", "abc")
	if err != ErrBadTimestamp {
		t.Errorf("error = %v, want ErrBadTimestamp", err)
	}
}
