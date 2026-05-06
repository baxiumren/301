# Cloudflare Redirect Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go Telegram bot that lets whitelisted team members change Cloudflare redirect destination URLs via inline keyboard — supporting both Redirect Rules API (versi 1) and Page Rules API (versi 2).

**Architecture:** Long-polling Go bot with 4 packages: `config` (YAML loading), `cloudflare` (API client for both CF API types), `bot` (handler + session store), and `main` (wiring). Session state is kept in-memory per user ID with 2-minute timeout.

**Tech Stack:** Go 1.21+, `go-telegram-bot-api/telegram-bot-api/v5`, `gopkg.in/yaml.v3`, Go standard `net/http` for Cloudflare calls.

---

## File Map

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point: load config, init CF client + bot handler, start polling loop |
| `config/config.go` | `Config` + `Domain` structs, `Load(path)` func |
| `config/config_test.go` | Tests for config loading |
| `cloudflare/client.go` | `Client` interface + `httpClient` impl: `GetCurrentURL`, `UpdateURL` for both API types |
| `cloudflare/client_test.go` | Tests with `httptest.NewServer` mock |
| `bot/session.go` | `SessionStore` — in-memory map, mutex, 2-min timeout, background cleanup |
| `bot/session_test.go` | Tests for set/get/delete/timeout |
| `bot/handler.go` | `Handler`: whitelist check, `/redirect`, `/status`, callback routing, URL input |
| `config.example.yaml` | Template config (committed) |
| `config.yaml` | Actual config with real tokens (gitignored) |
| `.gitignore` | Ignore `config.yaml`, binaries |

---

## Task 1: Project Setup

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `config.example.yaml`

- [ ] **Step 1: Initialize Go module**

```bash
cd E:\CODING\redirect
go mod init cf-redirect-bot
```

Expected: `go.mod` created with `module cf-redirect-bot` and `go 1.21` (or current version).

- [ ] **Step 2: Install dependencies**

```bash
go get github.com/go-telegram-bot-api/telegram-bot-api/v5
go get gopkg.in/yaml.v3
```

Expected: `go.sum` created, both packages added to `go.mod`.

- [ ] **Step 3: Create .gitignore**

Create file `.gitignore`:

```gitignore
config.yaml
cf-redirect-bot
cf-redirect-bot.exe
*.exe
```

- [ ] **Step 4: Create config.example.yaml**

Create file `config.example.yaml`:

```yaml
telegram:
  token: "BOT_TOKEN_DARI_BOTFATHER"

cloudflare:
  api_token: "CF_API_TOKEN_DENGAN_PERMISSION_EDIT_RULES"

whitelist:
  - 123456789
  - 987654321

domains:
  - name: "301maha.store"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "maha301.lol"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "maha55.id"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "maha66.id"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "mh301sl.store"
    zone_id: "ZONE_ID"
    type: "page_rules"
    rule_id: "RULE_ID"
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum .gitignore config.example.yaml
git commit -m "feat: project setup, go module, dependencies"
```

---

## Task 2: Config Package

**Files:**
- Create: `config/config.go`
- Create: `config/config_test.go`
- Create: `config/testdata/valid.yaml` (test fixture)

- [ ] **Step 1: Write failing test**

Create `config/config_test.go`:

```go
package config_test

import (
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
	if cfg.Cloudflare.APIToken != "cf-token" {
		t.Errorf("got api_token %q, want %q", cfg.Cloudflare.APIToken, "cf-token")
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
```

- [ ] **Step 2: Create test fixtures**

Create `config/testdata/valid.yaml`:

```yaml
telegram:
  token: "test-token"

cloudflare:
  api_token: "cf-token"

whitelist:
  - 111
  - 222

domains:
  - name: "example.com"
    zone_id: "zone1"
    type: "redirect_rules"
    ruleset_id: "rs1"
    rule_id: "r1"
  - name: "example2.com"
    zone_id: "zone2"
    type: "page_rules"
    rule_id: "r2"
```

Create `config/testdata/invalid.yaml`:

```yaml
telegram: [this is not valid: yaml: :::
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd E:\CODING\redirect
go test ./config/...
```

Expected: FAIL — `config.Load` undefined.

- [ ] **Step 4: Implement config package**

Create `config/config.go`:

```go
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
		APIToken string `yaml:"api_token"`
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
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./config/... -v
```

Expected:
```
--- PASS: TestLoad_ValidFile
--- PASS: TestLoad_MissingFile
--- PASS: TestLoad_InvalidYAML
PASS
```

- [ ] **Step 6: Commit**

```bash
git add config/ 
git commit -m "feat: config package with YAML loading"
```

---

## Task 3: Cloudflare Client

**Files:**
- Create: `cloudflare/client.go`
- Create: `cloudflare/client_test.go`

- [ ] **Step 1: Write failing tests**

Create `cloudflare/client_test.go`:

```go
package cloudflare_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cf-redirect-bot/cloudflare"
	"cf-redirect-bot/config"
)

func newTestClient(server *httptest.Server) cloudflare.Client {
	return cloudflare.NewWithBaseURL("test-token", server.URL)
}

func TestGetCurrentURL_RedirectRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("got method %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/rulesets/rs1") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"rules": []map[string]any{
					{
						"id": "r1",
						"action_parameters": map[string]any{
							"from_value": map[string]any{
								"target_url": map[string]any{
									"value": "https://target.com/page",
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	domain := config.Domain{
		ZoneID:    "z1",
		Type:      "redirect_rules",
		RulesetID: "rs1",
		RuleID:    "r1",
	}
	url, err := newTestClient(srv).GetCurrentURL(domain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://target.com/page" {
		t.Errorf("got %q, want %q", url, "https://target.com/page")
	}
}

func TestGetCurrentURL_PageRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"actions": []map[string]any{
					{
						"id": "forwarding_url",
						"value": map[string]any{
							"url":         "https://old-target.com/daftar",
							"status_code": 301,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	domain := config.Domain{
		ZoneID: "z1",
		Type:   "page_rules",
		RuleID: "r2",
	}
	url, err := newTestClient(srv).GetCurrentURL(domain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://old-target.com/daftar" {
		t.Errorf("got %q, want %q", url, "https://old-target.com/daftar")
	}
}

func TestUpdateURL_RedirectRules(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("got method %s, want PATCH", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer srv.Close()

	domain := config.Domain{
		ZoneID:    "z1",
		Type:      "redirect_rules",
		RulesetID: "rs1",
		RuleID:    "r1",
	}
	err := newTestClient(srv).UpdateURL(domain, "https://new-target.com/daftar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ap, _ := capturedBody["action_parameters"].(map[string]any)
	fv, _ := ap["from_value"].(map[string]any)
	tu, _ := fv["target_url"].(map[string]any)
	if tu["value"] != "https://new-target.com/daftar" {
		t.Errorf("request body missing correct target_url: %+v", capturedBody)
	}
}

func TestUpdateURL_PageRules(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer srv.Close()

	domain := config.Domain{
		ZoneID: "z1",
		Type:   "page_rules",
		RuleID: "r2",
	}
	err := newTestClient(srv).UpdateURL(domain, "https://new-target.com/daftar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	actions, _ := capturedBody["actions"].([]any)
	if len(actions) == 0 {
		t.Fatal("expected actions in request body")
	}
	action, _ := actions[0].(map[string]any)
	val, _ := action["value"].(map[string]any)
	if val["url"] != "https://new-target.com/daftar" {
		t.Errorf("request body missing correct url: %+v", capturedBody)
	}
}

func TestGetCurrentURL_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"success": false})
	}))
	defer srv.Close()

	domain := config.Domain{ZoneID: "z1", Type: "redirect_rules", RulesetID: "rs1", RuleID: "r1"}
	_, err := newTestClient(srv).GetCurrentURL(domain)
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./cloudflare/... 
```

Expected: FAIL — `cloudflare.Client` undefined.

- [ ] **Step 3: Implement cloudflare client**

Create `cloudflare/client.go`:

```go
package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"cf-redirect-bot/config"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

type Client interface {
	GetCurrentURL(domain config.Domain) (string, error)
	UpdateURL(domain config.Domain, newURL string) error
}

type httpClient struct {
	apiToken   string
	baseURL    string
	httpClient *http.Client
}

func New(apiToken string) Client {
	return &httpClient{
		apiToken:   apiToken,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func NewWithBaseURL(apiToken, baseURL string) Client {
	return &httpClient{
		apiToken:   apiToken,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *httpClient) do(method, url string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cloudflare API error: status %d", resp.StatusCode)
	}
	return data, nil
}

// --- Redirect Rules (versi 1) ---

type cfRulesetResponse struct {
	Success bool `json:"success"`
	Result  struct {
		Rules []struct {
			ID               string `json:"id"`
			ActionParameters struct {
				FromValue struct {
					TargetURL struct {
						Value string `json:"value"`
					} `json:"target_url"`
				} `json:"from_value"`
			} `json:"action_parameters"`
		} `json:"rules"`
	} `json:"result"`
}

type cfRedirectRuleBody struct {
	Action           string `json:"action"`
	ActionParameters struct {
		FromValue struct {
			TargetURL struct {
				Value string `json:"value"`
			} `json:"target_url"`
			StatusCode          int  `json:"status_code"`
			PreserveQueryString bool `json:"preserve_query_string"`
		} `json:"from_value"`
	} `json:"action_parameters"`
	Expression string `json:"expression"`
	Enabled    bool   `json:"enabled"`
}

func (c *httpClient) getRedirectRulesURL(d config.Domain) (string, error) {
	url := fmt.Sprintf("%s/zones/%s/rulesets/%s", c.baseURL, d.ZoneID, d.RulesetID)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	var result cfRulesetResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	for _, rule := range result.Result.Rules {
		if rule.ID == d.RuleID {
			return rule.ActionParameters.FromValue.TargetURL.Value, nil
		}
	}
	return "", fmt.Errorf("rule %s not found in ruleset %s", d.RuleID, d.RulesetID)
}

func (c *httpClient) updateRedirectRulesURL(d config.Domain, newURL string) error {
	var body cfRedirectRuleBody
	body.Action = "redirect"
	body.ActionParameters.FromValue.TargetURL.Value = newURL
	body.ActionParameters.FromValue.StatusCode = 301
	body.ActionParameters.FromValue.PreserveQueryString = false
	body.Expression = "true"
	body.Enabled = true

	url := fmt.Sprintf("%s/zones/%s/rulesets/%s/rules/%s", c.baseURL, d.ZoneID, d.RulesetID, d.RuleID)
	_, err := c.do(http.MethodPatch, url, body)
	return err
}

// --- Page Rules (versi 2) ---

type cfPageRuleResponse struct {
	Success bool `json:"success"`
	Result  struct {
		Actions []struct {
			ID    string `json:"id"`
			Value struct {
				URL        string `json:"url"`
				StatusCode int    `json:"status_code"`
			} `json:"value"`
		} `json:"actions"`
	} `json:"result"`
}

type cfPageRuleBody struct {
	Actions []struct {
		ID    string `json:"id"`
		Value struct {
			URL        string `json:"url"`
			StatusCode int    `json:"status_code"`
		} `json:"value"`
	} `json:"actions"`
}

func (c *httpClient) getPageRulesURL(d config.Domain) (string, error) {
	url := fmt.Sprintf("%s/zones/%s/pagerules/%s", c.baseURL, d.ZoneID, d.RuleID)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	var result cfPageRuleResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	for _, action := range result.Result.Actions {
		if action.ID == "forwarding_url" {
			return action.Value.URL, nil
		}
	}
	return "", fmt.Errorf("forwarding_url action not found in page rule %s", d.RuleID)
}

func (c *httpClient) updatePageRulesURL(d config.Domain, newURL string) error {
	var body cfPageRuleBody
	body.Actions = []struct {
		ID    string `json:"id"`
		Value struct {
			URL        string `json:"url"`
			StatusCode int    `json:"status_code"`
		} `json:"value"`
	}{
		{
			ID: "forwarding_url",
			Value: struct {
				URL        string `json:"url"`
				StatusCode int    `json:"status_code"`
			}{URL: newURL, StatusCode: 301},
		},
	}
	url := fmt.Sprintf("%s/zones/%s/pagerules/%s", c.baseURL, d.ZoneID, d.RuleID)
	_, err := c.do(http.MethodPatch, url, body)
	return err
}

// --- Public interface ---

func (c *httpClient) GetCurrentURL(domain config.Domain) (string, error) {
	switch domain.Type {
	case "redirect_rules":
		return c.getRedirectRulesURL(domain)
	case "page_rules":
		return c.getPageRulesURL(domain)
	default:
		return "", fmt.Errorf("unknown domain type: %s", domain.Type)
	}
}

func (c *httpClient) UpdateURL(domain config.Domain, newURL string) error {
	switch domain.Type {
	case "redirect_rules":
		return c.updateRedirectRulesURL(domain, newURL)
	case "page_rules":
		return c.updatePageRulesURL(domain, newURL)
	default:
		return fmt.Errorf("unknown domain type: %s", domain.Type)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./cloudflare/... -v
```

Expected:
```
--- PASS: TestGetCurrentURL_RedirectRules
--- PASS: TestGetCurrentURL_PageRules
--- PASS: TestUpdateURL_RedirectRules
--- PASS: TestUpdateURL_PageRules
--- PASS: TestGetCurrentURL_APIError
PASS
```

- [ ] **Step 5: Commit**

```bash
git add cloudflare/
git commit -m "feat: cloudflare client for Redirect Rules and Page Rules APIs"
```

---

## Task 4: Session Store

**Files:**
- Create: `bot/session.go`
- Create: `bot/session_test.go`

- [ ] **Step 1: Write failing tests**

Create `bot/session_test.go`:

```go
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

	store.Set(123, domain)

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
	store.Set(123, &config.Domain{Name: "example.com"})
	store.Delete(123)
	_, ok := store.Get(123)
	if ok {
		t.Fatal("expected session to be deleted")
	}
}

func TestSessionStore_Expiry(t *testing.T) {
	store := bot.NewSessionStoreWithTimeout(50 * time.Millisecond)
	store.Set(123, &config.Domain{Name: "example.com"})

	time.Sleep(100 * time.Millisecond)

	_, ok := store.Get(123)
	if ok {
		t.Fatal("expected session to be expired")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./bot/... 
```

Expected: FAIL — `bot.NewSessionStore` undefined.

- [ ] **Step 3: Implement session store**

Create `bot/session.go`:

```go
package bot

import (
	"sync"
	"time"

	"cf-redirect-bot/config"
)

type Session struct {
	Domain    *config.Domain
	ExpiresAt time.Time
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

func (s *SessionStore) Set(userID int64, domain *config.Domain) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[userID] = &Session{
		Domain:    domain,
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./bot/... -v
```

Expected:
```
--- PASS: TestSessionStore_SetAndGet
--- PASS: TestSessionStore_GetMissing
--- PASS: TestSessionStore_Delete
--- PASS: TestSessionStore_Expiry
PASS
```

- [ ] **Step 5: Commit**

```bash
git add bot/session.go bot/session_test.go
git commit -m "feat: session store with 2-minute timeout"
```

---

## Task 5: Bot Handler

**Files:**
- Create: `bot/handler.go`

- [ ] **Step 1: Implement handler**

Create `bot/handler.go`:

```go
package bot

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/cloudflare"
	"cf-redirect-bot/config"
)

type Handler struct {
	api      *tgbotapi.BotAPI
	cfg      *config.Config
	cf       cloudflare.Client
	sessions *SessionStore
}

func NewHandler(api *tgbotapi.BotAPI, cfg *config.Config, cf cloudflare.Client) *Handler {
	return &Handler{
		api:      api,
		cfg:      cfg,
		cf:       cf,
		sessions: NewSessionStore(),
	}
}

func (h *Handler) isAllowed(userID int64) bool {
	for _, id := range h.cfg.Whitelist {
		if id == userID {
			return true
		}
	}
	return false
}

func (h *Handler) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) sendWithKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) Handle(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		h.handleCallback(update.CallbackQuery)
		return
	}
	if update.Message == nil {
		return
	}

	userID := update.Message.From.ID
	if !h.isAllowed(userID) {
		h.send(update.Message.Chat.ID, "⛔ Kamu tidak memiliki akses untuk menggunakan command ini.")
		return
	}

	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "redirect":
			h.handleRedirectCommand(update.Message)
		case "status":
			h.handleStatusCommand(update.Message)
		}
		return
	}

	h.handleURLInput(update.Message)
}

func (h *Handler) domainKeyboard() tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for i, d := range h.cfg.Domains {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(d.Name, "domain:"+d.Name))
		if len(row) == 2 || i == len(h.cfg.Domains)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
	})
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) cancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
		},
	)
}

func (h *Handler) handleRedirectCommand(msg *tgbotapi.Message) {
	h.sendWithKeyboard(msg.Chat.ID, "🌐 Pilih domain yang mau diganti:", h.domainKeyboard())
}

func (h *Handler) handleStatusCommand(msg *tgbotapi.Message) {
	var sb strings.Builder
	sb.WriteString("📊 *Status Redirect Semua Domain:*\n\n")
	for _, d := range h.cfg.Domains {
		label := "Redirect Rules"
		if d.Type == "page_rules" {
			label = "Page Rules"
		}
		url, err := h.cf.GetCurrentURL(d)
		if err != nil {
			log.Printf("status error for %s: %v", d.Name, err)
			sb.WriteString(fmt.Sprintf("🔹 *%s* (%s)\n❌ Gagal mengambil data\n\n", d.Name, label))
			continue
		}
		sb.WriteString(fmt.Sprintf("🔹 *%s* (%s)\n→ %s\n\n", d.Name, label, url))
	}
	msg := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	msg.ParseMode = "Markdown"
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) handleCallback(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID
	ack := tgbotapi.NewCallback(cb.ID, "")
	h.api.Send(ack)

	if !h.isAllowed(userID) {
		h.send(cb.Message.Chat.ID, "⛔ Kamu tidak memiliki akses.")
		return
	}

	data := cb.Data

	if data == "cancel" {
		h.sessions.Delete(userID)
		h.send(cb.Message.Chat.ID, "🚫 Dibatalkan.")
		return
	}

	if strings.HasPrefix(data, "domain:") {
		domainName := strings.TrimPrefix(data, "domain:")
		var found *config.Domain
		for i := range h.cfg.Domains {
			if h.cfg.Domains[i].Name == domainName {
				found = &h.cfg.Domains[i]
				break
			}
		}
		if found == nil {
			h.send(cb.Message.Chat.ID, "❌ Domain tidak ditemukan.")
			return
		}

		currentURL, err := h.cf.GetCurrentURL(*found)
		if err != nil {
			log.Printf("get URL error for %s: %v", found.Name, err)
			currentURL = "(gagal mengambil URL saat ini)"
		}

		label := "Redirect Rules"
		if found.Type == "page_rules" {
			label = "Page Rules"
		}

		h.sessions.Set(userID, found)
		text := fmt.Sprintf("📌 *%s* (%s)\nURL sekarang: %s\n\nKirim URL tujuan baru (atau klik Cancel):", found.Name, label, currentURL)
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = h.cancelKeyboard()
		if _, err := h.api.Send(msg); err != nil {
			log.Printf("send error: %v", err)
		}
	}
}

func (h *Handler) handleURLInput(msg *tgbotapi.Message) {
	userID := msg.From.ID
	sess, ok := h.sessions.Get(userID)
	if !ok {
		return
	}

	newURL := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(newURL, "https://") {
		h.send(msg.Chat.ID, "⚠️ URL harus diawali dengan https://")
		return
	}

	if err := h.cf.UpdateURL(*sess.Domain, newURL); err != nil {
		log.Printf("update URL error for %s: %v", sess.Domain.Name, err)
		h.send(msg.Chat.ID, "❌ Gagal mengubah URL. Coba lagi.")
		return
	}

	h.sessions.Delete(userID)
	h.send(msg.Chat.ID, fmt.Sprintf("✅ Berhasil diubah!\nDomain : %s\nURL Baru: %s", sess.Domain.Name, newURL))
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./bot/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add bot/handler.go
git commit -m "feat: bot handler with /redirect, /status, inline keyboard, cancel"
```

---

## Task 6: Main Entry Point

**Files:**
- Create: `main.go`

- [ ] **Step 1: Implement main.go**

Create `main.go`:

```go
package main

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/bot"
	"cf-redirect-bot/cloudflare"
	"cf-redirect-bot/config"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	api, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		log.Fatalf("failed to connect to Telegram: %v", err)
	}
	log.Printf("Authorized on account %s", api.Self.UserName)

	cfClient := cloudflare.New(cfg.Cloudflare.APIToken)
	handler := bot.NewHandler(api, cfg, cfClient)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := api.GetUpdatesChan(u)

	log.Println("Bot started, listening for updates...")
	for update := range updates {
		handler.Handle(update)
	}
}
```

- [ ] **Step 2: Copy config.example.yaml to config.yaml dan isi dengan data asli**

```bash
copy config.example.yaml config.yaml
```

Edit `config.yaml` dengan:
- `telegram.token` → token dari BotFather
- `cloudflare.api_token` → CF API Token dengan permission **Zone > Rules > Edit**
- `whitelist` → Telegram User ID tim (bisa cek ID via @userinfobot)
- `domains[*].zone_id` → Zone ID tiap domain (ada di CF dashboard > Overview, kanan bawah)
- `domains[*].ruleset_id` → (untuk redirect_rules) ID ruleset (lihat cara dapat di bawah)
- `domains[*].rule_id` → ID rule spesifik (lihat cara dapat di bawah)

**Cara dapat Zone ID:** CF Dashboard → pilih domain → Overview → kanan bawah "Zone ID"

**Cara dapat Ruleset ID & Rule ID (Redirect Rules):**
```bash
curl -s -X GET "https://api.cloudflare.com/client/v4/zones/ZONE_ID/rulesets" \
  -H "Authorization: Bearer CF_API_TOKEN" | python -m json.tool
```
Cari ruleset dengan `phase: "http_request_dynamic_redirect"`, ambil `id` (= ruleset_id) dan `rules[0].id` (= rule_id).

**Cara dapat Rule ID (Page Rules):**
```bash
curl -s -X GET "https://api.cloudflare.com/client/v4/zones/ZONE_ID/pagerules" \
  -H "Authorization: Bearer CF_API_TOKEN" | python -m json.tool
```
Ambil `result[0].id` (= rule_id).

- [ ] **Step 3: Build dan run**

```bash
go build -o cf-redirect-bot.exe .
.\cf-redirect-bot.exe
```

Expected log output:
```
Authorized on account <bot_username>
Bot started, listening for updates...
```

- [ ] **Step 4: Manual test checklist di Telegram**

1. Tambahkan bot ke grup
2. Kirim `/redirect` → bot harus tampil domain list dengan tombol inline
3. Klik salah satu domain → bot tampil URL sekarang + tombol Cancel
4. Kirim URL baru `https://...` → bot konfirmasi berhasil
5. Cek di CF dashboard bahwa URL benar-benar berubah
6. Kirim `/redirect` lagi → klik domain → klik Cancel → bot balas "🚫 Dibatalkan."
7. Kirim `/status` → bot tampil semua domain dengan URL masing-masing
8. Test dari user yang tidak ada di whitelist → bot balas "⛔ Kamu tidak memiliki akses."
9. Kirim URL tanpa `https://` → bot balas "⚠️ URL harus diawali dengan https://"

- [ ] **Step 5: Run all tests satu kali lagi**

```bash
go test ./... -v
```

Expected: semua PASS.

- [ ] **Step 6: Final commit**

```bash
git add main.go
git commit -m "feat: main entry point, bot is ready to run"
```

---

## Self-Review

**Spec coverage check:**
- ✅ 5 domains dengan 2 tipe CF API
- ✅ `/redirect` command + inline keyboard domain selection
- ✅ `/status` command tampil semua domain
- ✅ Cancel button di setiap tahap flow
- ✅ Whitelist check di setiap command + callback
- ✅ Validasi URL harus `https://`
- ✅ Session in-memory per user ID dengan 2-minute timeout
- ✅ Error handling untuk CF API error
- ✅ Long polling (tidak butuh domain/SSL)
- ✅ config.yaml (gitignored) + config.example.yaml (committed)

**Type consistency check:**
- `config.Domain` dipakai konsisten di `cloudflare.Client`, `bot.SessionStore`, dan `bot.Handler`
- `cloudflare.Client` interface dipakai di `bot.Handler` dan di `main.go`
- `bot.NewSessionStore()` dan `bot.NewSessionStoreWithTimeout()` — keduanya return `*SessionStore`, konsisten dengan test
- `bot.NewHandler()` parameter: `*tgbotapi.BotAPI`, `*config.Config`, `cloudflare.Client` — konsisten dengan main.go
