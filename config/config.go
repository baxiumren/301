package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// CFAccount holds credentials for one Cloudflare account.
type CFAccount struct {
	Name   string `yaml:"name"`
	Email  string `yaml:"email"`
	APIKey string `yaml:"api_key"`
}

type Domain struct {
	Name      string `yaml:"name"`
	Label     string `yaml:"label,omitempty"`
	ZoneID    string `yaml:"zone_id"`
	Type      string `yaml:"type"`
	RulesetID string `yaml:"ruleset_id,omitempty"`
	RuleID    string `yaml:"rule_id"`
	CFAccount string `yaml:"cf_account,omitempty"`
}

type Config struct {
	Telegram struct {
		Token string `yaml:"token"`
	} `yaml:"telegram"`
	CFAccounts    []CFAccount `yaml:"cloudflare"`
	AllowedChatID int64       `yaml:"allowed_chat_id"`
	Domains       []Domain    `yaml:"domains"`
}

// FindAccount returns the CF account by name, or nil if not found.
func (c *Config) FindAccount(name string) *CFAccount {
	for i := range c.CFAccounts {
		if c.CFAccounts[i].Name == name {
			return &c.CFAccounts[i]
		}
	}
	return nil
}

// DefaultAccount returns the first CF account, or nil if none.
func (c *Config) DefaultAccount() *CFAccount {
	if len(c.CFAccounts) == 0 {
		return nil
	}
	return &c.CFAccounts[0]
}

// AccountForDomain returns the CF account for the given domain.
// Falls back to default account if domain has no cf_account set.
func (c *Config) AccountForDomain(d Domain) *CFAccount {
	if d.CFAccount != "" {
		if acc := c.FindAccount(d.CFAccount); acc != nil {
			return acc
		}
	}
	return c.DefaultAccount()
}

// Load reads config from path. Supports both old single-account format
// (cloudflare: {email, api_key}) and new multi-account format
// (cloudflare: [{name, email, api_key}]).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Peek at cloudflare field type to detect old vs new format
	var peek struct {
		CF interface{} `yaml:"cloudflare"`
	}
	yaml.Unmarshal(data, &peek) //nolint:errcheck

	if _, isMap := peek.CF.(map[string]interface{}); isMap {
		// Old single-account format: cloudflare: {email: ..., api_key: ...}
		type oldCFConfig struct {
			Telegram struct {
				Token string `yaml:"token"`
			} `yaml:"telegram"`
			Cloudflare struct {
				Email  string `yaml:"email"`
				APIKey string `yaml:"api_key"`
			} `yaml:"cloudflare"`
			AllowedChatID int64    `yaml:"allowed_chat_id"`
			Domains       []Domain `yaml:"domains"`
		}
		var old oldCFConfig
		if err := yaml.Unmarshal(data, &old); err != nil {
			return nil, err
		}
		cfg := &Config{}
		cfg.Telegram.Token = old.Telegram.Token
		cfg.AllowedChatID = old.AllowedChatID
		cfg.Domains = old.Domains
		if old.Cloudflare.Email != "" {
			cfg.CFAccounts = []CFAccount{{
				Name:   "default",
				Email:  old.Cloudflare.Email,
				APIKey: old.Cloudflare.APIKey,
			}}
			// Set cf_account = "default" for all existing domains
			for i := range cfg.Domains {
				if cfg.Domains[i].CFAccount == "" {
					cfg.Domains[i].CFAccount = "default"
				}
			}
		}
		return cfg, nil
	}

	// New multi-account format (or fresh config with no cloudflare key yet)
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
