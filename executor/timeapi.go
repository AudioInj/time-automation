package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/audioinj/time-automation/config"
	"github.com/audioinj/time-automation/notify"
)

// One browser operation at a time also protects the persisted cookie jar.
type Executor struct {
	cfg         config.Config
	mu          sync.Mutex
	nextAttempt time.Time
	run         func(context.Context, []byte) error
}

func New(cfg config.Config) *Executor {
	e := &Executor{cfg: cfg}
	e.run = e.runBrowser
	return e
}

func (e *Executor) VerboseLog(msg string) {
	if e.cfg.Verbose {
		log.Println("[VERBOSE]", msg)
	}
}

func (e *Executor) runBrowser(ctx context.Context, input []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	helper := e.cfg.BrowserHelper
	if helper == "" {
		helper = "browser/client.mjs"
	}
	cmd := exec.CommandContext(ctx, "node", helper)
	cmd.Stdin = bytes.NewReader(input)
	// Do not forward browser diagnostics: they can contain URLs, cookies or credentials.
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return errors.New("browser helper failed; check SSO credentials, connectivity and browser installation")
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.OK {
		return errors.New("browser operation was not confirmed")
	}
	return nil
}

func (e *Executor) perform(ctx context.Context, action string, status interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg.DryRun && action != "check" {
		log.Printf("[POST] DRY_RUN: %s", action)
		return nil
	}
	if time.Now().Before(e.nextAttempt) {
		return errors.New("authentication/API retry cooldown active")
	}
	origin := fmt.Sprintf("https://%s.%s", e.cfg.Subdomain, e.cfg.Domain)
	authOrigin := e.cfg.AuthOrigin
	if authOrigin == "" {
		authOrigin = "https://auth." + e.cfg.Domain
	}
	sessionFile := e.cfg.BrowserSessionFile
	if sessionFile == "" {
		sessionFile = filepath.Join(filepath.Dir(e.cfg.StateFile), "browser-session.json")
	}
	// The scheduler supports one action of each kind per local calendar day.
	// Stable across crashes, manual/scheduled execution and uncertain responses.
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(origin+"|"+e.cfg.Username+"|"+time.Now().Format("2006-01-02")+"|"+action)))
	task := e.cfg.Task
	if status == true {
		task = "Pauseneintrag - Status: Pause auto"
	}
	input, err := json.Marshal(map[string]interface{}{
		"origin": origin, "authOrigin": authOrigin, "username": e.cfg.Username, "password": e.cfg.Password,
		"sessionFile": sessionFile, "action": action, "status": status, "task": task, "key": key,
	})
	if err != nil {
		return err
	}
	if err := e.run(ctx, input); err != nil {
		e.nextAttempt = time.Now().Add(time.Minute)
		notify.New(e.cfg.WebhookURL).Send(ctx, "Time booking failed", "SSO/API operation failed; local booking remains pending.")
		return err
	}
	e.nextAttempt = time.Time{}
	return nil
}

func (e *Executor) CheckAuth(ctx context.Context) error  { return e.perform(ctx, "check", nil) }
func (e *Executor) StartWork(ctx context.Context) error  { return e.perform(ctx, "start_work", "Start") }
func (e *Executor) StopWork(ctx context.Context) error   { return e.perform(ctx, "stop_work", "Stop") }
func (e *Executor) StartBreak(ctx context.Context) error { return e.perform(ctx, "start_break", false) }
func (e *Executor) StopBreak(ctx context.Context) error  { return e.perform(ctx, "stop_break", true) }
