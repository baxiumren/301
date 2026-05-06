package bot

import (
	"sync"
	"time"

	"cf-redirect-bot/config"
)

type Session struct {
	Domain     *config.Domain
	OldURL     string
	PendingURL string
	ExpiresAt  time.Time
}

type SessionStore struct {
	mu      sync.Mutex
	store   map[int64]*Session
	timeout time.Duration
}

func NewSessionStore() *SessionStore {
	return NewSessionStoreWithTimeout(2 * time.Minute)
}

func NewSessionStoreWithTimeout(timeout time.Duration) *SessionStore {
	s := &SessionStore{
		store:   make(map[int64]*Session),
		timeout: timeout,
	}
	go s.cleanup()
	return s
}

func (s *SessionStore) Set(userID int64, domain *config.Domain, oldURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[userID] = &Session{
		Domain:    domain,
		OldURL:    oldURL,
		ExpiresAt: time.Now().Add(s.timeout),
	}
}

func (s *SessionStore) Get(userID int64) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.store[userID]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.store, userID)
		return nil, false
	}
	return sess, true
}

func (s *SessionStore) Delete(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, userID)
}

func (s *SessionStore) cleanup() {
	for range time.Tick(30 * time.Second) {
		s.mu.Lock()
		for id, sess := range s.store {
			if time.Now().After(sess.ExpiresAt) {
				delete(s.store, id)
			}
		}
		s.mu.Unlock()
	}
}
