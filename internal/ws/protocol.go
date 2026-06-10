package ws

import "encoding/json"

// Message is the envelope for all WebSocket messages.
// Agent→Server and Dashboard→Server use: {"event": "...", "data": {...}}
// Server→Dashboard uses the same format.
// Server→Agent uses cmdsig.SignedEnvelope (sent as "signed_command" event).
type Message struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

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
