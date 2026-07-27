package database

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SMSStore provides encrypted CRUD for SMS threads/messages. All plaintext
// (message bodies, thread snippets) is encrypted with AES-256-GCM before it
// touches gorm/the database — callers only ever see plaintext via the DTOs
// this type returns.
type SMSStore struct {
	db  *gorm.DB
	key []byte
}

// NewSMSStore wraps a GORM DB with encrypted SMS persistence. key must be a
// 32-byte AES-256 key (see LoadOrCreateKey).
func NewSMSStore(db *gorm.DB, key []byte) *SMSStore {
	return &SMSStore{db: db, key: key}
}

// ThreadDTO is the JSON-safe, decrypted projection of a Thread.
type ThreadDTO struct {
	ThreadID      uint      `json:"threadId"`
	Address       string    `json:"address"`
	DisplayName   string    `json:"displayName,omitempty"`
	Snippet       string    `json:"snippet"`
	UnreadCount   int       `json:"unreadCount"`
	LastMessageAt time.Time `json:"lastMessageAt"`
}

// MessageDTO is the JSON-safe, decrypted projection of a Message.
type MessageDTO struct {
	MessageID uint      `json:"messageId"`
	ThreadID  uint      `json:"threadId"`
	Address   string    `json:"address"`
	Direction string    `json:"direction"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// UpsertThread creates or updates the thread for (clientID, address),
// refreshing its snippet/last-message-at and optionally bumping unread
// count. Returns the thread's ID.
func (s *SMSStore) UpsertThread(clientID, address, snippetPlain string, ts time.Time, incrementUnread bool) (uint, error) {
	cipherSnippet, err := seal(s.key, []byte(snippetPlain))
	if err != nil {
		return 0, fmt.Errorf("encrypt snippet: %w", err)
	}

	var thread Thread
	err = s.db.Where("client_id = ? AND address = ?", clientID, address).First(&thread).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		thread = Thread{
			ClientID:      clientID,
			Address:       address,
			Snippet:       cipherSnippet,
			LastMessageAt: ts,
		}
		if incrementUnread {
			thread.UnreadCount = 1
		}
		if err := s.db.Create(&thread).Error; err != nil {
			return 0, fmt.Errorf("create thread: %w", err)
		}
		return thread.ID, nil
	case err != nil:
		return 0, fmt.Errorf("lookup thread: %w", err)
	}

	updates := map[string]any{"snippet": cipherSnippet, "last_message_at": ts}
	if incrementUnread {
		updates["unread_count"] = gorm.Expr("unread_count + 1")
	}
	if err := s.db.Model(&thread).Updates(updates).Error; err != nil {
		return 0, fmt.Errorf("update thread: %w", err)
	}
	return thread.ID, nil
}

// InsertMessage stores a message, encrypting its body. Idempotent on
// (ClientID, RemoteID): a retried delivery returns the existing message's ID
// without erroring or double-inserting.
func (s *SMSStore) InsertMessage(clientID string, threadID uint, remoteID, address, direction, bodyPlain, status string, ts time.Time) (uint, error) {
	var existing Message
	err := s.db.Where("client_id = ? AND remote_id = ?", clientID, remoteID).First(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("lookup message: %w", err)
	}

	cipherBody, err := seal(s.key, []byte(bodyPlain))
	if err != nil {
		return 0, fmt.Errorf("encrypt message body: %w", err)
	}

	msg := Message{
		ThreadID:  threadID,
		ClientID:  clientID,
		RemoteID:  remoteID,
		Address:   address,
		Direction: direction,
		Body:      cipherBody,
		Status:    status,
		Timestamp: ts,
	}
	if err := s.db.Create(&msg).Error; err != nil {
		return 0, fmt.Errorf("create message: %w", err)
	}
	return msg.ID, nil
}

// UpdateMessageStatus updates a message's delivery status (e.g. sent -> delivered/failed).
func (s *SMSStore) UpdateMessageStatus(clientID, remoteID, status string) error {
	return s.db.Model(&Message{}).
		Where("client_id = ? AND remote_id = ?", clientID, remoteID).
		Update("status", status).Error
}

// ListThreads returns a client's threads, most recently active first.
func (s *SMSStore) ListThreads(clientID string, limit int) ([]ThreadDTO, error) {
	if limit <= 0 {
		limit = 50
	}
	var threads []Thread
	if err := s.db.Where("client_id = ?", clientID).
		Order("last_message_at DESC").Limit(limit).Find(&threads).Error; err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}

	out := make([]ThreadDTO, 0, len(threads))
	for _, t := range threads {
		snippet, err := open(s.key, t.Snippet)
		if err != nil {
			snippet = []byte("(unable to decrypt)")
		}
		out = append(out, ThreadDTO{
			ThreadID:      t.ID,
			Address:       t.Address,
			DisplayName:   t.DisplayName,
			Snippet:       string(snippet),
			UnreadCount:   t.UnreadCount,
			LastMessageAt: t.LastMessageAt,
		})
	}
	return out, nil
}

// ListMessages returns a thread's messages, oldest first.
func (s *SMSStore) ListMessages(clientID string, threadID uint, limit int) ([]MessageDTO, error) {
	if limit <= 0 {
		limit = 200
	}
	var messages []Message
	if err := s.db.Where("client_id = ? AND thread_id = ?", clientID, threadID).
		Order("timestamp ASC").Limit(limit).Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	out := make([]MessageDTO, 0, len(messages))
	for _, m := range messages {
		body, err := open(s.key, m.Body)
		if err != nil {
			body = []byte("(unable to decrypt)")
		}
		out = append(out, MessageDTO{
			MessageID: m.ID,
			ThreadID:  m.ThreadID,
			Address:   m.Address,
			Direction: m.Direction,
			Body:      string(body),
			Status:    m.Status,
			Timestamp: m.Timestamp,
		})
	}
	return out, nil
}

// FindThreadIDByAddress looks up a thread's ID for (clientID, address).
func (s *SMSStore) FindThreadIDByAddress(clientID, address string) (uint, bool) {
	var thread Thread
	if err := s.db.Select("id").Where("client_id = ? AND address = ?", clientID, address).First(&thread).Error; err != nil {
		return 0, false
	}
	return thread.ID, true
}
