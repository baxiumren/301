package bot

import (
	"sort"
	"sync"

	"cf-redirect-bot/config"
)

type BulkSession struct {
	Selected   map[string]bool
	MessageID  int
	ChatID     int64
	PendingURL string
	Phase      string // "selecting" | "awaiting_url" | "confirming"
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
	return &BulkStore{store: make(map[int64]*BulkSession)}
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
	}
}

func (b *BulkStore) Toggle(userID int64, domainName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess, ok := b.store[userID]; ok {
		sess.Selected[domainName] = !sess.Selected[domainName]
	}
}

func (b *BulkStore) Get(userID int64) (*BulkSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sess, ok := b.store[userID]
	return sess, ok
}

func (b *BulkStore) SetAwaitingURL(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess, ok := b.store[userID]; ok {
		sess.Phase = "awaiting_url"
	}
}

func (b *BulkStore) SetPendingURL(userID int64, url string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess, ok := b.store[userID]; ok {
		sess.PendingURL = url
		sess.Phase = "confirming"
	}
}

func (b *BulkStore) Delete(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.store, userID)
}
