package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cf-redirect-bot/config"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

// DiscoveredRule: hasil auto-fetch redirect rule dari Cloudflare API.
type DiscoveredRule struct {
	RulesetID string
	RuleID    string
	TargetURL string
}

// DiscoveredPageRule: hasil auto-fetch page rule dari Cloudflare API.
type DiscoveredPageRule struct {
	RuleID    string
	Pattern   string // URL pattern (target URL pattern)
	TargetURL string // forwarding target
}

// ZoneInfo hasil menambahkan domain baru ke Cloudflare.
type ZoneInfo struct {
	ZoneID      string
	NameServers []string
}

type Client interface {
	GetCurrentURL(domain config.Domain) (string, error)
	UpdateURL(domain config.Domain, newURL string) error
	// Discovery: auto-fetch semua IDs dari CF API
	GetZoneID(domainName string) (string, error)
	ListRedirectRules(zoneID string) ([]DiscoveredRule, error)
	ListPageRules(zoneID string) ([]DiscoveredPageRule, error)
	// New domain: daftarkan domain baru ke CF + setup
	AddZone(domainName string) (ZoneInfo, error)
	DeleteZone(zoneID string) error
	GetZoneStatus(zoneID string) (string, error)
	ListDNSRecords(zoneID string) ([]DNSRecord, error)
	CreateDNSRecord(zoneID, recordType, name, content string, proxied bool) error
	DeleteDNSRecord(zoneID, recordID string) error
	UpdateDNSRecord(zoneID, recordID, recordType, name, content string, proxied bool) error
	CreatePageRule(zoneID, pattern, targetURL string) (string, error)
	CreateRedirectRuleV2(zoneID, targetURL string) (rulesetID, ruleID string, err error)
}

type httpClient struct {
	email      string
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func New(email, apiKey string) Client {
	return &httpClient{
		email:      email,
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func NewWithBaseURL(email, apiKey, baseURL string) Client {
	return &httpClient{
		email:      email,
		apiKey:     apiKey,
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
	req.Header.Set("X-Auth-Email", c.email)
	req.Header.Set("X-Auth-Key", c.apiKey)
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
		// Coba extract pesan error dari CF response
		var cfErr struct {
			Errors []struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"errors"`
		}
		if json.Unmarshal(data, &cfErr) == nil && len(cfErr.Errors) > 0 {
			return nil, fmt.Errorf("CF error %d: %s", cfErr.Errors[0].Code, cfErr.Errors[0].Message)
		}
		return nil, fmt.Errorf("cloudflare API error: status %d body: %s", resp.StatusCode, string(data))
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

// --- Discovery: zone ID lookup ---

type cfZonesResponse struct {
	Success bool `json:"success"`
	Result  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"result"`
}

func (c *httpClient) GetZoneID(domainName string) (string, error) {
	// Strip subdomain — cari root domain
	// Cloudflare zones terdaftar per root domain (contoh: domain.com bukan sub.domain.com)
	root := domainName
	parts := strings.Split(domainName, ".")
	if len(parts) > 2 {
		root = strings.Join(parts[len(parts)-2:], ".")
	}

	url := fmt.Sprintf("%s/zones?name=%s", c.baseURL, root)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("gagal cari zone: %w", err)
	}
	var resp cfZonesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if len(resp.Result) == 0 {
		return "", fmt.Errorf("zone tidak ditemukan untuk domain: %s", domainName)
	}
	return resp.Result[0].ID, nil
}

// --- Discovery: list rulesets & rules ---

type cfRulesetsListResponse struct {
	Success bool `json:"success"`
	Result  []struct {
		ID    string `json:"id"`
		Phase string `json:"phase"`
	} `json:"result"`
}

func (c *httpClient) ListRedirectRules(zoneID string) ([]DiscoveredRule, error) {
	// 1. Ambil daftar rulesets, cari yang fase redirect
	url := fmt.Sprintf("%s/zones/%s/rulesets", c.baseURL, zoneID)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal ambil rulesets: %w", err)
	}
	var listResp cfRulesetsListResponse
	if err := json.Unmarshal(data, &listResp); err != nil {
		return nil, err
	}

	var results []DiscoveredRule
	for _, rs := range listResp.Result {
		if rs.Phase != "http_request_dynamic_redirect" {
			continue
		}
		// 2. Ambil rules di dalam ruleset ini
		rsURL := fmt.Sprintf("%s/zones/%s/rulesets/%s", c.baseURL, zoneID, rs.ID)
		rsData, err := c.do(http.MethodGet, rsURL, nil)
		if err != nil {
			continue
		}
		var rsResp cfRulesetResponse
		if err := json.Unmarshal(rsData, &rsResp); err != nil {
			continue
		}
		for _, rule := range rsResp.Result.Rules {
			results = append(results, DiscoveredRule{
				RulesetID: rs.ID,
				RuleID:    rule.ID,
				TargetURL: rule.ActionParameters.FromValue.TargetURL.Value,
			})
		}
	}
	return results, nil
}

type cfPageRulesListResponse struct {
	Success bool `json:"success"`
	Result  []struct {
		ID      string `json:"id"`
		Targets []struct {
			Constraint struct {
				Value string `json:"value"`
			} `json:"constraint"`
		} `json:"targets"`
		Actions []struct {
			ID    string `json:"id"`
			Value struct {
				URL        string `json:"url"`
				StatusCode int    `json:"status_code"`
			} `json:"value"`
		} `json:"actions"`
	} `json:"result"`
}

func (c *httpClient) ListPageRules(zoneID string) ([]DiscoveredPageRule, error) {
	url := fmt.Sprintf("%s/zones/%s/pagerules?status=active", c.baseURL, zoneID)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal ambil page rules: %w", err)
	}
	var resp cfPageRulesListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	var results []DiscoveredPageRule
	for _, pr := range resp.Result {
		pattern := ""
		if len(pr.Targets) > 0 {
			pattern = pr.Targets[0].Constraint.Value
		}
		targetURL := ""
		for _, action := range pr.Actions {
			if action.ID == "forwarding_url" {
				targetURL = action.Value.URL
				break
			}
		}
		results = append(results, DiscoveredPageRule{
			RuleID:    pr.ID,
			Pattern:   pattern,
			TargetURL: targetURL,
		})
	}
	return results, nil
}

// --- New domain: add zone + create redirect rule ---

type cfAddZoneRequest struct {
	Name      string `json:"name"`
	JumpStart bool   `json:"jump_start"`
}

type cfAddZoneResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID          string   `json:"id"`
		NameServers []string `json:"name_servers"`
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *httpClient) AddZone(domainName string) (ZoneInfo, error) {
	body := cfAddZoneRequest{Name: domainName, JumpStart: false}
	data, err := c.do(http.MethodPost, c.baseURL+"/zones", body)
	if err != nil {
		return ZoneInfo{}, fmt.Errorf("gagal daftarkan domain: %w", err)
	}
	var resp cfAddZoneResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return ZoneInfo{}, err
	}
	if !resp.Success {
		msg := "gagal menambahkan zone"
		if len(resp.Errors) > 0 {
			msg = resp.Errors[0].Message
		}
		return ZoneInfo{}, fmt.Errorf("%s", msg)
	}
	return ZoneInfo{
		ZoneID:      resp.Result.ID,
		NameServers: resp.Result.NameServers,
	}, nil
}

// DNSRecord representasi DNS record dari Cloudflare.
type DNSRecord struct {
	ID      string
	Type    string
	Name    string
	Content string
	Proxied bool
}

func (c *httpClient) ListDNSRecords(zoneID string) ([]DNSRecord, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records", c.baseURL, zoneID)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal ambil DNS records: %w", err)
	}
	var resp struct {
		Result []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
			Proxied bool   `json:"proxied"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	var records []DNSRecord
	for _, r := range resp.Result {
		records = append(records, DNSRecord{
			ID:      r.ID,
			Type:    r.Type,
			Name:    r.Name,
			Content: r.Content,
			Proxied: r.Proxied,
		})
	}
	return records, nil
}

func (c *httpClient) DeleteZone(zoneID string) error {
	url := fmt.Sprintf("%s/zones/%s", c.baseURL, zoneID)
	_, err := c.do(http.MethodDelete, url, nil)
	return err
}

func (c *httpClient) GetZoneStatus(zoneID string) (string, error) {
	url := fmt.Sprintf("%s/zones/%s", c.baseURL, zoneID)
	data, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("gagal cek status zone: %w", err)
	}
	var resp struct {
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	return resp.Result.Status, nil
}

func (c *httpClient) CreateDNSRecord(zoneID, recordType, name, content string, proxied bool) error {
	ttl := 1 // Auto TTL (wajib untuk proxied)
	if !proxied {
		ttl = 3600 // 1 jam untuk non-proxied
	}
	body := map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": content,
		"proxied": proxied,
		"ttl":     ttl,
	}
	url := fmt.Sprintf("%s/zones/%s/dns_records", c.baseURL, zoneID)
	_, err := c.do(http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("gagal buat DNS record: %w", err)
	}
	return nil
}

func (c *httpClient) DeleteDNSRecord(zoneID, recordID string) error {
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", c.baseURL, zoneID, recordID)
	_, err := c.do(http.MethodDelete, url, nil)
	return err
}

func (c *httpClient) UpdateDNSRecord(zoneID, recordID, recordType, name, content string, proxied bool) error {
	ttl := 1 // Auto TTL (wajib untuk proxied)
	if !proxied {
		ttl = 3600
	}
	body := map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": content,
		"proxied": proxied,
		"ttl":     ttl,
	}
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", c.baseURL, zoneID, recordID)
	_, err := c.do(http.MethodPut, url, body)
	return err
}

func (c *httpClient) CreatePageRule(zoneID, pattern, targetURL string) (string, error) {
	body := map[string]interface{}{
		"targets": []map[string]interface{}{
			{
				"target": "url",
				"constraint": map[string]interface{}{
					"operator": "matches",
					"value":    pattern,
				},
			},
		},
		"actions": []map[string]interface{}{
			{
				"id": "forwarding_url",
				"value": map[string]interface{}{
					"url":         targetURL,
					"status_code": 301,
				},
			},
		},
		"status": "active",
	}
	url := fmt.Sprintf("%s/zones/%s/pagerules", c.baseURL, zoneID)
	data, err := c.do(http.MethodPost, url, body)
	if err != nil {
		return "", fmt.Errorf("gagal buat page rule: %w", err)
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if !resp.Success || resp.Result.ID == "" {
		return "", fmt.Errorf("gagal membuat page rule")
	}
	return resp.Result.ID, nil
}

type cfCreateRulesetRequest struct {
	Name  string        `json:"name"`
	Kind  string        `json:"kind"`
	Phase string        `json:"phase"`
	Rules []interface{} `json:"rules"`
}

type cfCreateRulesetResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID    string `json:"id"`
		Rules []struct {
			ID string `json:"id"`
		} `json:"rules"`
	} `json:"result"`
}

func (c *httpClient) CreateRedirectRuleV2(zoneID, targetURL string) (rulesetID, ruleID string, err error) {
	rule := map[string]interface{}{
		"action": "redirect",
		"action_parameters": map[string]interface{}{
			"from_value": map[string]interface{}{
				"target_url":           map[string]interface{}{"value": targetURL},
				"status_code":          301,
				"preserve_query_string": false,
			},
		},
		"expression": "true",
		"enabled":    true,
	}

	// Cek apakah sudah ada redirect ruleset
	existing, _ := c.ListRedirectRules(zoneID)
	if len(existing) >= 0 {
		// Coba ambil ruleset ID yang sudah ada
		listURL := fmt.Sprintf("%s/zones/%s/rulesets", c.baseURL, zoneID)
		ldata, lerr := c.do(http.MethodGet, listURL, nil)
		if lerr == nil {
			var listResp cfRulesetsListResponse
			if json.Unmarshal(ldata, &listResp) == nil {
				for _, rs := range listResp.Result {
					if rs.Phase == "http_request_dynamic_redirect" {
						// Tambahkan rule ke ruleset yang sudah ada
						addURL := fmt.Sprintf("%s/zones/%s/rulesets/%s/rules", c.baseURL, zoneID, rs.ID)
						rdata, rerr := c.do(http.MethodPost, addURL, rule)
						if rerr == nil {
							var rresp struct {
								Result struct {
									Rules []struct{ ID string `json:"id"` } `json:"rules"`
								} `json:"result"`
							}
							if json.Unmarshal(rdata, &rresp) == nil && len(rresp.Result.Rules) > 0 {
								lastRule := rresp.Result.Rules[len(rresp.Result.Rules)-1]
								return rs.ID, lastRule.ID, nil
							}
						}
					}
				}
			}
		}
	}

	// Buat ruleset baru dengan rule di dalamnya
	body := map[string]interface{}{
		"name":  "Redirect Rules",
		"kind":  "zone",
		"phase": "http_request_dynamic_redirect",
		"rules": []interface{}{rule},
	}
	url := fmt.Sprintf("%s/zones/%s/rulesets", c.baseURL, zoneID)
	data, err := c.do(http.MethodPost, url, body)
	if err != nil {
		return "", "", fmt.Errorf("gagal buat redirect rule: %w", err)
	}
	var resp cfCreateRulesetResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", err
	}
	if !resp.Success || len(resp.Result.Rules) == 0 {
		return "", "", fmt.Errorf("gagal membuat redirect ruleset")
	}
	return resp.Result.ID, resp.Result.Rules[0].ID, nil
}

// --- Public interface dispatch ---

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
