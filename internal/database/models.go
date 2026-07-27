package database

import "time"

// Thread is one SMS conversation with a single address, scoped to the phone
// agent that owns it (ClientID) since a fleet can have more than one phone.
type Thread struct {
	ID            uint      `gorm:"primaryKey"`
	ClientID      string    `gorm:"index;uniqueIndex:idx_thread_client_address;size:191"`
	Address       string    `gorm:"uniqueIndex:idx_thread_client_address;size:191"`
	DisplayName   string `gorm:"size:191"`
	// Snippet is a preview of the last message, AES-256-GCM ciphertext like
	// Message.Body — it's still message content, so it gets the same at-rest
	// protection rather than leaking plaintext via the "cheap" preview field.
	Snippet     []byte
	UnreadCount int
	LastMessageAt time.Time `gorm:"index"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Message is a single SMS, in either direction. Body is stored as
// AES-256-GCM ciphertext (see crypto.go) — never logged, never returned
// as plaintext outside SMSStore's decrypt path.
type Message struct {
	ID        uint   `gorm:"primaryKey"`
	ThreadID  uint   `gorm:"index"`
	ClientID  string `gorm:"index;uniqueIndex:idx_message_client_remote;size:191"`
	// RemoteID identifies the message on the phone side: the companion app's
	// generated messageId for outbound sends, or a deterministic hash of
	// (clientId, address, body, timestamp) for inbound pushes that didn't
	// carry one — either way, unique per (ClientID, RemoteID) so a retried
	// push/relay can't double-insert.
	RemoteID  string `gorm:"uniqueIndex:idx_message_client_remote;size:191"`
	Address   string `gorm:"size:191"`
	Direction string `gorm:"size:8"` // "in" | "out"
	Body      []byte // AES-256-GCM ciphertext
	Status    string `gorm:"size:16"` // queued|sent|delivered|failed|received
	Timestamp time.Time `gorm:"index"`
	CreatedAt time.Time
}
