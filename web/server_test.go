package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/audioinj/time-automation/config"
	"github.com/audioinj/time-automation/tracker"
)

func makeServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	state := tracker.New(dir + "/state.json")
	return New(config.Config{
		Domain:    "example.com",
		Subdomain: "time",
		Username:  "testuser",
		WorkDays:  "1-5",
		Task:      "Dev",
	}, state, ":0")
}

func TestIndexReturns200(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html Content-Type, got %q", ct)
	}
}

func TestIndexUnknownPathReturns404(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()
	s.handleIndex(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAPIStatusReturnsJSON(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	s.handleAPIStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var data StatusData
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

func TestAPIStatusContainsConfig(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	s.handleAPIStatus(w, req)

	var data StatusData
	_ = json.Unmarshal(w.Body.Bytes(), &data)

	if data.Config.Username != "testuser" {
		t.Errorf("expected username testuser, got %q", data.Config.Username)
	}
	if data.Config.Endpoint != "time.example.com" {
		t.Errorf("expected endpoint time.example.com, got %q", data.Config.Endpoint)
	}
}

func TestAPIStatusNoPasswordInResponse(t *testing.T) {
	dir := t.TempDir()
	state := tracker.New(dir + "/state.json")
	s := New(config.Config{
		Password:   "supersecret",
		WebhookURL: "https://discord.com/api/webhooks/secret",
	}, state, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	s.handleAPIStatus(w, req)

	body := w.Body.String()
	if strings.Contains(body, "supersecret") {
		t.Error("password must not appear in API response")
	}
	if strings.Contains(body, "discord.com") {
		t.Error("webhook URL must not appear in API response")
	}
}

func TestIndexRendersStatusBadges(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleIndex(w, req)

	body := w.Body.String()
	// The page must always contain these structural elements
	for _, want := range []string{"Tagesstatus", "Nettoarbeitszeit", "Tagesplan", "Konfiguration"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected HTML to contain %q", want)
		}
	}
}
