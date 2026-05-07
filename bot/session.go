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

// redirectAwaitDomain: store user yang sedang menunggu input pilih domain untuk redirect.
var redirectAwaitDomain = struct {
	mu    sync.Mutex
	store map[int64]bool
}{store: make(map[int64]bool)}

func setRedirectAwaitDomain(userID int64) {
	redirectAwaitDomain.mu.Lock()
	defer redirectAwaitDomain.mu.Unlock()
	redirectAwaitDomain.store[userID] = true
}

func hasRedirectAwaitDomain(userID int64) bool {
	redirectAwaitDomain.mu.Lock()
	defer redirectAwaitDomain.mu.Unlock()
	return redirectAwaitDomain.store[userID]
}

func deleteRedirectAwaitDomain(userID int64) {
	redirectAwaitDomain.mu.Lock()
	defer redirectAwaitDomain.mu.Unlock()
	delete(redirectAwaitDomain.store, userID)
}

// rollbackAwait: store user yang sedang menunggu input pilih entry history untuk rollback.
var rollbackAwait = struct {
	mu    sync.Mutex
	store map[int64]bool
}{store: make(map[int64]bool)}

func setRollbackAwait(userID int64) {
	rollbackAwait.mu.Lock()
	defer rollbackAwait.mu.Unlock()
	rollbackAwait.store[userID] = true
}

func hasRollbackAwait(userID int64) bool {
	rollbackAwait.mu.Lock()
	defer rollbackAwait.mu.Unlock()
	return rollbackAwait.store[userID]
}

func deleteRollbackAwait(userID int64) {
	rollbackAwait.mu.Lock()
	defer rollbackAwait.mu.Unlock()
	delete(rollbackAwait.store, userID)
}

// removeDomainAwait: store user yang menunggu konfirmasi hapus domain.
var removeDomainAwait = struct {
	mu    sync.Mutex
	store map[int64]string // userID → nama domain yang mau dihapus
}{store: make(map[int64]string)}

func setRemoveDomainAwait(userID int64, domainName string) {
	removeDomainAwait.mu.Lock()
	defer removeDomainAwait.mu.Unlock()
	removeDomainAwait.store[userID] = domainName
}

func getRemoveDomainAwait(userID int64) (string, bool) {
	removeDomainAwait.mu.Lock()
	defer removeDomainAwait.mu.Unlock()
	name, ok := removeDomainAwait.store[userID]
	return name, ok
}

func deleteRemoveDomainAwait(userID int64) {
	removeDomainAwait.mu.Lock()
	defer removeDomainAwait.mu.Unlock()
	delete(removeDomainAwait.store, userID)
}

// removeDomainSelectAwait: flag user yang sedang memilih domain mana yang mau dihapus.
var removeDomainSelectAwait = struct {
	mu    sync.Mutex
	store map[int64]bool
}{store: make(map[int64]bool)}

func setRemoveDomainSelectAwait(userID int64) {
	removeDomainSelectAwait.mu.Lock()
	defer removeDomainSelectAwait.mu.Unlock()
	removeDomainSelectAwait.store[userID] = true
}

func hasRemoveDomainSelectAwait(userID int64) bool {
	removeDomainSelectAwait.mu.Lock()
	defer removeDomainSelectAwait.mu.Unlock()
	return removeDomainSelectAwait.store[userID]
}

func deleteRemoveDomainSelectAwait(userID int64) {
	removeDomainSelectAwait.mu.Lock()
	defer removeDomainSelectAwait.mu.Unlock()
	delete(removeDomainSelectAwait.store, userID)
}
