package bot

import (
	"sort"
	"sync"
	"time"

	"cf-redirect-bot/config"
)

const bulkTimeout = 5 * time.Minute

type BulkSession struct {
	Selected   map[string]bool
	MessageID  int
	ChatID     int64
	PendingURL string
	Phase      string // "selecting" | "awaiting_url" | "confirming"
	ExpiresAt  time.Time
}

func (s *BulkSession) SelectedNames() []string {
	var names []string
	for name, sel := range s.Selected {
		if sel {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

type BulkStore struct {
	mu    sync.Mutex
	store map[int64]*BulkSession
}

func NewBulkStore() *BulkStore {
	b := &BulkStore{store: make(map[int64]*BulkSession)}
	go b.cleanup()
	return b
}

func (b *BulkStore) New(userID, chatID int64, messageID int, domains []config.Domain) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sel := make(map[string]bool, len(domains))
	for _, d := range domains {
		sel[d.Name] = false
	}
	b.store[userID] = &BulkSession{
		Selected:  sel,
		MessageID: messageID,
		ChatID:    chatID,
		Phase:     "selecting",
		ExpiresAt: time.Now().Add(bulkTimeout),
	}
}

func (b *BulkStore) Toggle(userID int64, domainName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess, ok := b.store[userID]; ok {
		sess.Selected[domainName] = !sess.Selected[domainName]
		sess.ExpiresAt = time.Now().Add(bulkTimeout) // refresh on activity
	}
}

func (b *BulkStore) Get(userID int64) (*BulkSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sess, ok := b.store[userID]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(b.store, userID)
		return nil, false
	}
	return sess, ok
}

func (b *BulkStore) SetAwaitingURL(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess, ok := b.store[userID]; ok {
		sess.Phase = "awaiting_url"
		sess.ExpiresAt = time.Now().Add(bulkTimeout)
	}
}

func (b *BulkStore) SetPendingURL(userID int64, url string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess, ok := b.store[userID]; ok {
		sess.PendingURL = url
		sess.Phase = "confirming"
		sess.ExpiresAt = time.Now().Add(bulkTimeout)
	}
}

func (b *BulkStore) Delete(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.store, userID)
}

func (b *BulkStore) cleanup() {
	for range time.Tick(30 * time.Second) {
		b.mu.Lock()
		for id, sess := range b.store {
			if time.Now().After(sess.ExpiresAt) {
				delete(b.store, id)
			}
		}
		b.mu.Unlock()
	}
}
