package bot_test

import (
	"testing"
	"time"

	"cf-redirect-bot/bot"
	"cf-redirect-bot/config"
)

func TestSessionStore_SetAndGet(t *testing.T) {
	store := bot.NewSessionStore()
	domain := &config.Domain{Name: "example.com"}

	store.Set(123, domain, "https://old.example.com")

	sess, ok := store.Get(123)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if sess.Domain.Name != "example.com" {
		t.Errorf("got domain %q, want %q", sess.Domain.Name, "example.com")
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	store := bot.NewSessionStore()
	_, ok := store.Get(999)
	if ok {
		t.Fatal("expected no session for unknown user")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := bot.NewSessionStore()
	store.Set(123, &config.Domain{Name: "example.com"}, "https://old.example.com")
	store.Delete(123)
	_, ok := store.Get(123)
	if ok {
		t.Fatal("expected session to be deleted")
	}
}

func TestSessionStore_Expiry(t *testing.T) {
	store := bot.NewSessionStoreWithTimeout(50 * time.Millisecond)
	store.Set(123, &config.Domain{Name: "example.com"}, "https://old.example.com")

	time.Sleep(100 * time.Millisecond)

	_, ok := store.Get(123)
	if ok {
		t.Fatal("expected session to be expired")
	}
}
