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
