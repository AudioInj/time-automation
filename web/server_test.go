package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	}, state, nil, nil, ":0")
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
	}, state, nil, nil, ":0")

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

func TestActionStartWork(t *testing.T) {
	s := makeServer(t)
	today := time.Now().Format("2006-01-02")

	req := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader("action=start_work"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleAction(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", w.Code)
	}
	st := s.state.Load(today)
	if !st.WorkStarted {
		t.Error("expected WorkStarted=true after start_work action")
	}
	if st.WorkStartTime.IsZero() {
		t.Error("expected WorkStartTime to be set")
	}
}

func TestActionRejectsGet(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/action", nil)
	w := httptest.NewRecorder()
	s.handleAction(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestActionUnknown(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader("action=invalid"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleAction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}
	if body := w.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("expected {\"status\":\"ok\"}, got %q", body)
	}
}

func TestIndexRendersStatusBadges(t *testing.T) {
	s := makeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleIndex(w, req)

	body := w.Body.String()
	// The page must always contain these structural elements
	for _, want := range []string{"Tagesstatus", "Nettoarbeitszeit", "Tagesplan", "Einstellungen"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected HTML to contain %q", want)
		}
	}
}
