package ws

import (
	"encoding/json"
	"testing"
)

func TestEncode(t *testing.T) {
	data := map[string]any{"clientId": "node-1", "ts": float64(1234567890)}
	raw, err := Encode("stats", data)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if msg.Event != "stats" {
		t.Errorf("Event = %q, want stats", msg.Event)
	}
}

func TestDecode(t *testing.T) {
	raw := []byte(`{"event":"pong","data":{"ts":1234567890}}`)
	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if msg.Event != "pong" {
		t.Errorf("Event = %q, want pong", msg.Event)
	}

	var data map[string]any
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("unmarshal data error = %v", err)
	}
	if data["ts"] != float64(1234567890) {
		t.Errorf("ts = %v", data["ts"])
	}
}

func TestDecode_InvalidJSON(t *testing.T) {
	_, err := Decode([]byte(`{not valid}`))
	if err == nil {
		t.Error("Decode() should fail on invalid JSON")
	}
}

func TestEncode_Decode_Roundtrip(t *testing.T) {
	data := map[string]string{"session": "abc-123", "data": "hello"}
	raw, err := Encode("shell_output", data)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if msg.Event != "shell_output" {
		t.Errorf("Event = %q, want shell_output", msg.Event)
	}

	var parsed map[string]string
	if err := json.Unmarshal(msg.Data, &parsed); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if parsed["session"] != "abc-123" {
		t.Errorf("session = %q", parsed["session"])
	}
}
