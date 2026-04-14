// executor/timeapi.go
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/audioinj/time-automation/config"
	"github.com/audioinj/time-automation/notify"
)

type Executor struct {
	cfg        config.Config
	token      string
	notifier   *notify.Notifier
	client     *http.Client
	retrySleep time.Duration
}

func New(cfg config.Config) *Executor {
	return &Executor{
		cfg:        cfg,
		notifier:   notify.New(cfg.WebhookURL),
		client:     &http.Client{Timeout: 30 * time.Second},
		retrySleep: time.Second,
	}
}

func (e *Executor) VerboseLog(msg string) {
	if e.cfg.Verbose {
		log.Println("[VERBOSE]", msg)
	}
}

func (e *Executor) login(ctx context.Context) string {
	payload := map[string]string{
		"username": e.cfg.Username,
		"password": e.cfg.Password,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[LOGIN] Failed to marshal payload: %v", err)
		return ""
	}
	url := fmt.Sprintf("https://%s.%s/api/login", e.cfg.Subdomain, e.cfg.Domain)
	log.Println("[LOGIN] Attempting login at:", url)
	e.VerboseLog("POST " + url)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		msg := "Login failed: " + err.Error()
		log.Printf("[LOGIN] %s", msg)
		e.notifier.Send(ctx, "Login Failed", msg)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		msg := "Login failed: " + err.Error()
		log.Printf("[LOGIN] %s", msg)
		e.notifier.Send(ctx, "Login Failed", msg)
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck

	var rawResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawResp); err != nil {
		log.Println("[LOGIN] Failed to decode response:", err)
	}
	if rawBytes, err := json.Marshal(rawResp); err == nil {
		e.VerboseLog("Login response: " + string(rawBytes))
	}

	token, _ := rawResp["token"].(string)
	if token == "" {
		msg := "Login failed: no token received"
		log.Printf("[LOGIN] %s", msg)
		e.notifier.Send(ctx, "Login Failed", msg)
	} else {
		log.Println("[LOGIN] Token received successfully")
	}
	return token
}

func (e *Executor) post(ctx context.Context, status interface{}) {
	e.VerboseLog(fmt.Sprintf("post: status=%v", status))
	if e.cfg.DryRun {
		log.Println("[POST] DRY_RUN enabled: would POST /api/post-time")
		e.VerboseLog("DRY_RUN enabled: would POST /api/post-time with status: " + toString(status))
		e.VerboseLog("Payload: " + toString(map[string]interface{}{
			"status":     status,
			"inputValue": e.cfg.Task,
			"userid":     e.cfg.Username,
		}))
		return
	}

	var payload map[string]interface{}
	task := e.cfg.Task

	// Bash logic: for break stop (status == true), override task
	if status == true {
		task = "Pauseneintrag - Status: Pause auto"
	}

	// Bash logic: status is string for work, bool for break
	switch v := status.(type) {
	case string:
		// "Start" or "Stop" for work
		payload = map[string]interface{}{
			"status":     v,
			"inputValue": task,
			"userid":     e.cfg.Username,
		}
	case bool:
		// true/false for break
		payload = map[string]interface{}{
			"status":     v,
			"inputValue": task,
			"userid":     e.cfg.Username,
		}
	default:
		// fallback
		payload = map[string]interface{}{
			"status":     status,
			"inputValue": task,
			"userid":     e.cfg.Username,
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[POST] Failed to marshal payload: %v", err)
		return
	}
	url := fmt.Sprintf("https://%s.%s/api/post-time", e.cfg.Subdomain, e.cfg.Domain)
	e.VerboseLog("POST " + url + " payload: " + string(data))

	var resp *http.Response
	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Re-login if token is missing or was cleared after a 401
		if e.token == "" {
			log.Printf("[POST] No token (attempt %d/%d), logging in...", attempt, maxRetries)
			e.token = e.login(ctx)
			if e.token == "" {
				log.Printf("[POST] Login failed on attempt %d, retrying...", attempt)
				select {
				case <-time.After(e.retrySleep):
				case <-ctx.Done():
					return
				}
				continue
			}
		}

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(data))
		req.Header.Set("Authorization", e.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err = e.client.Do(req)
		if err != nil {
			log.Printf("[POST] Attempt %d failed: %v", attempt, err)
			select {
			case <-time.After(e.retrySleep):
			case <-ctx.Done():
				return
			}
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break // success: body closed via defer below
		}
		if resp.StatusCode == 401 {
			log.Printf("[POST] Attempt %d: token expired (401), re-authenticating...", attempt)
			_ = resp.Body.Close()
			e.token = ""
			e.token = e.login(ctx)
			if e.token == "" {
				log.Println("[POST] Re-authentication failed, aborting")
				return
			}
		} else {
			log.Printf("[POST] Attempt %d failed: status %s", attempt, resp.Status)
			_ = resp.Body.Close()
		}
		select {
		case <-time.After(e.retrySleep):
		case <-ctx.Done():
			return
		}
	}
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var msg string
		if err != nil {
			msg = "Failed to post after 5 attempts: " + err.Error()
		} else if resp != nil {
			msg = "Failed to post after 5 attempts: HTTP status " + resp.Status
		} else {
			msg = "Failed to post after 5 attempts: unknown error"
		}
		log.Println("[POST] " + msg)
		e.notifier.Send(ctx, "Post Failed @here", msg)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	log.Printf("[POST] Posted status: %v", status)
	e.VerboseLog("Posted status: " + toString(status))
}

func toString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (e *Executor) StartWork(ctx context.Context)  { e.post(ctx, "Start") }
func (e *Executor) StopWork(ctx context.Context)   { e.post(ctx, "Stop") }
func (e *Executor) StartBreak(ctx context.Context) { e.post(ctx, false) }
func (e *Executor) StopBreak(ctx context.Context)  { e.post(ctx, true) }
