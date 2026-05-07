package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"cf-redirect-bot/config"
)

func TestLoad_ValidFile(t *testing.T) {
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Telegram.Token != "test-token" {
		t.Errorf("got token %q, want %q", cfg.Telegram.Token, "test-token")
	}
	if len(cfg.CFAccounts) == 0 {
		t.Fatal("expected at least 1 CF account")
	}
	acc := cfg.CFAccounts[0]
	if acc.Email != "test@example.com" {
		t.Errorf("got email %q, want %q", acc.Email, "test@example.com")
	}
	if acc.APIKey != "cf-key" {
		t.Errorf("got api_key %q, want %q", acc.APIKey, "cf-key")
	}
	if cfg.AllowedChatID != -123456789 {
		t.Errorf("got allowed_chat_id = %d, want -123456789", cfg.AllowedChatID)
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

func TestLoad_MigrationOldFormat(t *testing.T) {
	oldYAML := `
telegram:
  token: "old-token"
cloudflare:
  email: "old@example.com"
  api_key: "old-key"
allowed_chat_id: -999
domains:
  - name: "old.com"
    zone_id: "z1"
    type: "redirect_rules"
    ruleset_id: "rs1"
    rule_id: "r1"
`
	tmp := filepath.Join(t.TempDir(), "old.yaml")
	if err := os.WriteFile(tmp, []byte(oldYAML), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if len(cfg.CFAccounts) != 1 {
		t.Fatalf("expected 1 CF account after migration, got %d", len(cfg.CFAccounts))
	}
	acc := cfg.CFAccounts[0]
	if acc.Name != "default" {
		t.Errorf("migrated account name = %q, want %q", acc.Name, "default")
	}
	if acc.Email != "old@example.com" {
		t.Errorf("migrated email = %q, want %q", acc.Email, "old@example.com")
	}
	if cfg.Domains[0].CFAccount != "default" {
		t.Errorf("migrated domain[0].CFAccount = %q, want %q", cfg.Domains[0].CFAccount, "default")
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
