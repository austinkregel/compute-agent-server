package ws

import (
	"encoding/json"
	"time"
)

// Message is the envelope for all WebSocket messages.
// Agent→Server and Dashboard→Server use: {"event": "...", "data": {...}}
// Server→Dashboard uses the same format.
// Server→Agent uses cmdsig.SignedEnvelope (sent as "signed_command" event).
type Message struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// WriteTimeout bounds a single frame write to one peer.
//
// Every write needs a deadline because the transport blocks until the peer
// drains it: a browser tab or agent that has stopped reading (asleep, wedged,
// TCP window full) would otherwise block the writing goroutine forever. That
// goroutine is often not the stalled peer's own — dashboard broadcasts are
// driven from the agent read loop, so one unresponsive dashboard could stall
// an entire agent's event pipeline.
//
// The transport closes the connection when a write's context expires (see
// nhooyr.io/websocket.Conn: "On any error from any method, the connection is
// closed... This applies to context expirations as well"). That is the
// intended outcome here — drop the stalled peer and let it reconnect — so the
// bound is deliberately generous, leaving room for a large frame (shell
// output, 256 KiB file chunks) over a slow link before we call a peer dead.
//
// A var rather than a const only so tests can shrink it; nothing in production
// reassigns it.
var WriteTimeout = 10 * time.Second

// Encode serializes a message for sending over WebSocket.
func Encode(event string, data any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Message{Event: event, Data: raw})
}

// Decode parses a raw WebSocket message into a Message.
func Decode(raw []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
