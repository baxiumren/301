package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Domain struct {
	Name      string `yaml:"name"`
	ZoneID    string `yaml:"zone_id"`
	Type      string `yaml:"type"`
	RulesetID string `yaml:"ruleset_id"`
	RuleID    string `yaml:"rule_id"`
}

type Config struct {
	Telegram struct {
		Token string `yaml:"token"`
	} `yaml:"telegram"`
	Cloudflare struct {
		Email  string `yaml:"email"`
		APIKey string `yaml:"api_key"`
	} `yaml:"cloudflare"`
	Whitelist []int64  `yaml:"whitelist"`
	Domains   []Domain `yaml:"domains"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
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
