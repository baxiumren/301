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
