package config_test

import (
	"cf-redirect-bot/config"
	"testing"
)

func TestLoad_ValidFile(t *testing.T) {
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Telegram.Token != "test-token" {
		t.Errorf("got token %q, want %q", cfg.Telegram.Token, "test-token")
	}
	if cfg.Cloudflare.Email != "test@example.com" {
		t.Errorf("got email %q, want %q", cfg.Cloudflare.Email, "test@example.com")
	}
	if cfg.Cloudflare.APIKey != "cf-key" {
		t.Errorf("got api_key %q, want %q", cfg.Cloudflare.APIKey, "cf-key")
	}
	if len(cfg.Whitelist) != 2 {
		t.Errorf("got %d whitelist entries, want 2", len(cfg.Whitelist))
	}
	if cfg.Whitelist[0] != 111 {
		t.Errorf("got whitelist[0] = %d, want 111", cfg.Whitelist[0])
	}
	if len(cfg.Domains) != 2 {
		t.Errorf("got %d domains, want 2", len(cfg.Domains))
	}
	d := cfg.Domains[0]
	if d.Name != "example.com" || d.ZoneID != "zone1" || d.Type != "redirect_rules" || d.RulesetID != "rs1" || d.RuleID != "r1" {
		t.Errorf("domain[0] mismatch: %+v", d)
	}
	d2 := cfg.Domains[1]
	if d2.Name != "example2.com" || d2.Type != "page_rules" || d2.RuleID != "r2" {
		t.Errorf("domain[1] mismatch: %+v", d2)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("testdata/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	_, err := config.Load("testdata/invalid.yaml")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}
