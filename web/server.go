// web/server.go
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/audioinj/time-automation/config"
	"github.com/audioinj/time-automation/tracker"
)

// Executor is the minimal interface the web server needs to fire manual actions.
type Executor interface {
	StartWork(ctx context.Context)
	StopWork(ctx context.Context)
	StartBreak(ctx context.Context)
	StopBreak(ctx context.Context)
}

// Server serves the status web UI.
type Server struct {
	cfg   config.Config
	state *tracker.StateTracker
	exec  Executor
	srv   *http.Server
}

// New creates a Server that listens on addr (e.g. ":8077").
func New(cfg config.Config, state *tracker.StateTracker, exec Executor, addr string) *Server {
	s := &Server{cfg: cfg, state: state, exec: exec}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleAPIStatus)
	mux.HandleFunc("/api/action", s.handleAction)
	mux.HandleFunc("/health", s.handleHealth)
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// Start begins listening and blocks until ctx is cancelled, then shuts down gracefully.
func (s *Server) Start(ctx context.Context) {
	go func() {
		log.Printf("[WEB] Status UI available at http://0.0.0.0%s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[WEB] Server error: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[WEB] Shutdown error: %v", err)
	}
}

// --- data model ---

// HistoryEntry holds a summary of a single past day for the history table.
type HistoryEntry struct {
	Date      string `json:"date"`
	NetWork   string `json:"net_work"`
	Status    string `json:"status"`
	WorkStart string `json:"work_start"`
	WorkEnd   string `json:"work_end"`
}

// StatusData is passed to both the JSON API and the HTML template.
type StatusData struct {
	Date string    `json:"date"`
	Now  time.Time `json:"now"`

	// Computed status helpers for the template
	WorkActive   bool `json:"work_active"`
	BreakActive  bool `json:"break_active"`
	WorkComplete bool `json:"work_complete"`
	DayOff       bool `json:"day_off"`

	// State fields
	WorkStarted    bool   `json:"work_started"`
	WorkStopped    bool   `json:"work_stopped"`
	BreakStarted   bool   `json:"break_started"`
	BreakStopped   bool   `json:"break_stopped"`
	IsHoliday      bool   `json:"is_holiday"`
	IsVacation     bool   `json:"is_vacation"`
	DayNote        string `json:"day_note"`
	WorkStartTime  string `json:"work_start_time"`
	WorkStopTime   string `json:"work_stop_time"`
	BreakStartTime string `json:"break_start_time"`
	BreakStopTime  string `json:"break_stop_time"`
	NetWork        string `json:"net_work"`

	// Planned times (formatted)
	PlannedStartWork  string `json:"planned_start_work"`
	PlannedStartBreak string `json:"planned_start_break"`
	PlannedStopBreak  string `json:"planned_stop_break"`
	PlannedStopWork   string `json:"planned_stop_work"`

	// History (last 7 days, excluding today)
	History []HistoryEntry `json:"history"`

	// Available manual actions for the current state
	CanStartWork  bool `json:"can_start_work"`
	CanStopWork   bool `json:"can_stop_work"`
	CanStartBreak bool `json:"can_start_break"`
	CanStopBreak  bool `json:"can_stop_break"`

	Config ConfigSummary `json:"config"`
}

// ConfigSummary is a sanitised view of config.Config (no passwords or secrets).
type ConfigSummary struct {
	Endpoint         string `json:"endpoint"`
	Username         string `json:"username"`
	WorkDays         string `json:"work_days"`
	StartWorkMin     string `json:"start_work_min"`
	StartWorkMax     string `json:"start_work_max"`
	StartBreakMin    string `json:"start_break_min"`
	StartBreakMax    string `json:"start_break_max"`
	MinWorkDuration  string `json:"min_work_duration"`
	MaxWorkDuration  string `json:"max_work_duration"`
	MinBreakDuration string `json:"min_break_duration"`
	MaxBreakDuration string `json:"max_break_duration"`
	Task             string `json:"task"`
	DryRun           bool   `json:"dry_run"`
	Verbose          bool   `json:"verbose"`
	WebhookSet       bool   `json:"webhook_set"`
	HolidaySet       bool   `json:"holiday_set"`
	VacationSet      bool   `json:"vacation_set"`
	StateFile        string `json:"state_file"`
	ICSCacheDir      string `json:"ics_cache_dir"`
}

func fmtT(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("15:04:05")
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %02dm", h, m)
}

func buildHistory(all map[string]tracker.DayState, today string) []HistoryEntry {
	// Collect dates from the last 7 days (excluding today), sorted descending
	var dates []string
	for k := range all {
		if k != today {
			dates = append(dates, k)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) > 7 {
		dates = dates[:7]
	}

	entries := make([]HistoryEntry, 0, len(dates))
	for _, d := range dates {
		st := all[d]
		status := "—"
		switch {
		case st.IsHoliday:
			status = "Feiertag"
		case st.IsVacation:
			status = "Urlaub"
		case st.WorkStarted && st.WorkStopped && st.BreakStarted && st.BreakStopped:
			status = "Abgeschlossen"
		case st.WorkStarted:
			status = "Teilweise"
		}

		workEnd := st.WorkStopTime
		if workEnd.IsZero() && st.WorkStopped {
			workEnd = st.PlannedStopWork
		}

		entries = append(entries, HistoryEntry{
			Date:      d,
			NetWork:   fmtDur(st.NetWorkDuration()),
			Status:    status,
			WorkStart: fmtT(st.WorkStartTime),
			WorkEnd:   fmtT(workEnd),
		})
	}
	return entries
}

func (s *Server) buildStatus() StatusData {
	now := time.Now()
	today := now.Format("2006-01-02")
	st := s.state.Load(today)
	history := buildHistory(s.state.LoadAll(), today)

	return StatusData{
		Date: today,
		Now:  now,

		WorkActive:   st.WorkStarted && !st.WorkStopped,
		BreakActive:  st.BreakStarted && !st.BreakStopped,
		WorkComplete: st.WorkStarted && st.WorkStopped && st.BreakStarted && st.BreakStopped,
		DayOff:       st.IsHoliday || st.IsVacation,

		WorkStarted:    st.WorkStarted,
		WorkStopped:    st.WorkStopped,
		BreakStarted:   st.BreakStarted,
		BreakStopped:   st.BreakStopped,
		IsHoliday:      st.IsHoliday,
		IsVacation:     st.IsVacation,
		DayNote:        st.DayNote,
		WorkStartTime:  fmtT(st.WorkStartTime),
		WorkStopTime:   fmtT(st.WorkStopTime),
		BreakStartTime: fmtT(st.BreakStartTime),
		BreakStopTime:  fmtT(st.BreakStopTime),
		NetWork:        fmtDur(st.NetWorkDuration()),

		PlannedStartWork:  fmtT(st.PlannedStartWork),
		PlannedStartBreak: fmtT(st.PlannedStartBreak),
		PlannedStopBreak:  fmtT(st.PlannedStopBreak),
		PlannedStopWork:   fmtT(st.PlannedStopWork),

		History: history,

		CanStartWork:  !st.WorkStarted && !st.IsHoliday && !st.IsVacation,
		CanStopWork:   st.WorkStarted && !st.WorkStopped,
		CanStartBreak: st.WorkStarted && !st.WorkStopped && !st.BreakStarted,
		CanStopBreak:  st.BreakStarted && !st.BreakStopped,

		Config: ConfigSummary{
			Endpoint:         s.cfg.Subdomain + "." + s.cfg.Domain,
			Username:         s.cfg.Username,
			WorkDays:         s.cfg.WorkDays,
			StartWorkMin:     s.cfg.StartWorkMin.Format("15:04"),
			StartWorkMax:     s.cfg.StartWorkMax.Format("15:04"),
			StartBreakMin:    s.cfg.StartBreakMin.Format("15:04"),
			StartBreakMax:    s.cfg.StartBreakMax.Format("15:04"),
			MinWorkDuration:  s.cfg.MinWorkDuration.String(),
			MaxWorkDuration:  s.cfg.MaxWorkDuration.String(),
			MinBreakDuration: s.cfg.MinBreakDuration.String(),
			MaxBreakDuration: s.cfg.MaxBreakDuration.String(),
			Task:             s.cfg.Task,
			DryRun:           s.cfg.DryRun,
			Verbose:          s.cfg.Verbose,
			WebhookSet:       s.cfg.WebhookURL != "",
			HolidaySet:       s.cfg.HolidayAddress != "",
			VacationSet:      s.cfg.VacationAddress != "",
			StateFile:        s.cfg.StateFile,
			ICSCacheDir:      s.cfg.ICSCacheDir,
		},
	}
}

// --- handlers ---

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := r.FormValue("action")
	today := time.Now().Format("2006-01-02")
	st := s.state.Load(today)
	now := time.Now()

	switch action {
	case "start_work":
		if !st.WorkStarted {
			if s.exec != nil {
				s.exec.StartWork(r.Context())
			}
			st.WorkStarted = true
			st.WorkStartTime = now
			s.state.Save(today, st)
			log.Printf("[WEB] Manual action: start_work at %s", now.Format("15:04:05"))
		}
	case "stop_work":
		if st.WorkStarted && !st.WorkStopped {
			if s.exec != nil {
				s.exec.StopWork(r.Context())
			}
			st.WorkStopped = true
			st.WorkStopTime = now
			s.state.Save(today, st)
			log.Printf("[WEB] Manual action: stop_work at %s", now.Format("15:04:05"))
		}
	case "start_break":
		if st.WorkStarted && !st.BreakStarted {
			if s.exec != nil {
				s.exec.StartBreak(r.Context())
			}
			st.BreakStarted = true
			st.BreakStartTime = now
			s.state.Save(today, st)
			log.Printf("[WEB] Manual action: start_break at %s", now.Format("15:04:05"))
		}
	case "stop_break":
		if st.BreakStarted && !st.BreakStopped {
			if s.exec != nil {
				s.exec.StopBreak(r.Context())
			}
			st.BreakStopped = true
			st.BreakStopTime = now
			s.state.Save(today, st)
			log.Printf("[WEB] Manual action: stop_break at %s", now.Format("15:04:05"))
		}
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		log.Printf("[WEB] Health write error: %v", err)
	}
}

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.buildStatus()); err != nil {
		log.Printf("[WEB] JSON encode error: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, s.buildStatus()); err != nil {
		log.Printf("[WEB] Template error: %v", err)
	}
}

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

const indexHTML = `<!DOCTYPE html>
<html lang="de">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>Time Automation</title>
<style>
:root{
  --primary:#1e3a5f;--primary-light:#2d5a9e;
  --green:#16a34a;--amber:#d97706;--red:#dc2626;--blue:#2563eb;
  --green-bg:#dcfce7;--green-fg:#166534;
  --amber-bg:#fef9c3;--amber-fg:#854d0e;
  --red-bg:#fee2e2;--red-fg:#991b1b;
  --grey-bg:#f3f4f6;--grey-fg:#4b5563;
  --blue-bg:#dbeafe;--blue-fg:#1e40af;
  --muted:#6b7280;--border:#e5e7eb;--bg:#f1f5f9;
}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:var(--bg);color:#111;min-height:100vh}

/* Header */
header{background:var(--primary);color:#fff;padding:14px 28px;display:flex;justify-content:space-between;align-items:center;box-shadow:0 2px 6px rgba(0,0,0,.25)}
header h1{font-size:1.05rem;font-weight:600;letter-spacing:.03em}
header .meta{font-size:.75rem;opacity:.65;text-align:right;line-height:1.6}

/* Grid */
main{max-width:920px;margin:24px auto;padding:0 16px;display:grid;grid-template-columns:1fr 1fr;gap:16px}
.full{grid-column:1/-1}

/* Cards */
.card{background:#fff;border-radius:10px;padding:22px 24px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
.card h2{font-size:.68rem;font-weight:700;text-transform:uppercase;letter-spacing:.09em;color:var(--muted);margin-bottom:16px}

/* Badges */
.badge{display:inline-flex;align-items:center;gap:6px;padding:4px 12px;border-radius:99px;font-size:.8rem;font-weight:600;white-space:nowrap}
.badge .dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.badge-green{background:var(--green-bg);color:var(--green-fg)}.badge-green .dot{background:var(--green)}
.badge-amber{background:var(--amber-bg);color:var(--amber-fg)}.badge-amber .dot{background:var(--amber)}
.badge-red{background:var(--red-bg);color:var(--red-fg)}.badge-red .dot{background:var(--red)}
.badge-grey{background:var(--grey-bg);color:var(--grey-fg)}.badge-grey .dot{background:#9ca3af}
.badge-blue{background:var(--blue-bg);color:var(--blue-fg)}.badge-blue .dot{background:var(--blue)}

/* Status card */
.status-rows{display:flex;flex-direction:column;gap:10px}
.status-row{display:flex;justify-content:space-between;align-items:center;font-size:.875rem}
.status-row .label{color:var(--muted);font-size:.82rem}
.status-hint{font-size:.75rem;color:var(--muted);margin-top:12px;padding-top:12px;border-top:1px solid var(--border)}

/* Big number */
.bignum{font-size:3rem;font-weight:800;color:var(--primary);line-height:1;margin:8px 0 4px}
.bignum-label{font-size:.75rem;color:var(--muted)}
.bignum-note{font-size:.78rem;color:var(--muted);margin-top:10px}

/* Timeline */
.timeline{display:flex;flex-direction:column;gap:0}
.tl-row{display:flex;align-items:stretch;gap:0}
.tl-left{display:flex;flex-direction:column;align-items:center;width:32px;flex-shrink:0}
.tl-dot{width:13px;height:13px;border-radius:50%;flex-shrink:0;margin-top:3px}
.tl-line{width:2px;background:var(--border);flex:1;margin:2px 0}
.tl-row:last-child .tl-line{display:none}
.tl-dot-done{background:var(--green)}
.tl-dot-active{background:var(--amber);box-shadow:0 0 0 3px var(--amber-bg)}
.tl-dot-pending{background:#d1d5db}
.tl-content{padding:0 0 18px 10px;flex:1}
.tl-name{font-size:.875rem;font-weight:600;color:#111}
.tl-times{display:flex;gap:24px;margin-top:3px;flex-wrap:wrap}
.tl-time-item{display:flex;flex-direction:column;gap:1px}
.tl-time-item .tlabel{font-size:.7rem;color:var(--muted)}
.tl-time-item .tval{font-size:.83rem;font-family:monospace;font-weight:500}
.tl-status{font-size:.75rem;margin-top:4px}

/* Config table */
.cfg-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 32px}
.cfg-row{display:flex;justify-content:space-between;align-items:baseline;padding:7px 0;border-bottom:1px solid var(--border);font-size:.855rem}
.cfg-row:last-child{border-bottom:none}
.cfg-row .k{color:var(--muted);font-size:.8rem}
.cfg-row .v{font-weight:500;font-family:monospace;font-size:.82rem;text-align:right;max-width:60%}

/* History table */
.hist-table{width:100%;border-collapse:collapse;font-size:.855rem}
.hist-table th{text-align:left;font-size:.72rem;font-weight:700;text-transform:uppercase;letter-spacing:.07em;color:var(--muted);padding:0 12px 8px 0;border-bottom:2px solid var(--border)}
.hist-table td{padding:8px 12px 8px 0;border-bottom:1px solid var(--border);vertical-align:middle}
.hist-table tr:last-child td{border-bottom:none}
.hist-table td:last-child,.hist-table th:last-child{padding-right:0}
.hist-date{font-family:monospace;font-weight:600}
.hist-net{font-family:monospace;font-weight:700;color:var(--primary)}

/* Action buttons */
.actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:4px}
.btn{display:inline-flex;align-items:center;gap:6px;padding:8px 16px;border:none;border-radius:8px;font-size:.855rem;font-weight:600;cursor:pointer;transition:opacity .15s}
.btn:active{opacity:.75}
.btn-green{background:var(--green);color:#fff}
.btn-amber{background:var(--amber);color:#fff}
.btn-red{background:var(--red);color:#fff}
.btn-grey{background:#6b7280;color:#fff}

/* Flags row */
.flags{display:flex;gap:8px;flex-wrap:wrap;margin-top:4px}

@media(max-width:580px){
  main{grid-template-columns:1fr}
  .full{grid-column:1}
  .cfg-grid{grid-template-columns:1fr}
  .bignum{font-size:2.4rem}
}
</style>
</head>
<body>
<header>
  <h1>⏱ Time Automation</h1>
  <div class="meta">
    {{.Date}}<br>
    Aktualisiert {{.Now.Format "15:04:05"}}
  </div>
</header>
<main>

<!-- ── Tagesstatus ── -->
<div class="card">
  <h2>Tagesstatus</h2>
  <div class="status-rows">

    {{if .DayOff}}
    <div class="status-row">
      <span class="label">Heute</span>
      {{if .IsHoliday}}
        <span class="badge badge-blue"><span class="dot"></span>Feiertag</span>
      {{else}}
        <span class="badge badge-blue"><span class="dot"></span>Urlaub</span>
      {{end}}
    </div>
    {{else}}

    <div class="status-row">
      <span class="label">Arbeit</span>
      {{if .WorkActive}}
        <span class="badge badge-green"><span class="dot"></span>läuft</span>
      {{else if .WorkStopped}}
        <span class="badge badge-grey"><span class="dot"></span>beendet</span>
      {{else}}
        <span class="badge badge-grey"><span class="dot"></span>nicht gestartet</span>
      {{end}}
    </div>

    <div class="status-row">
      <span class="label">Pause</span>
      {{if .BreakActive}}
        <span class="badge badge-amber"><span class="dot"></span>läuft</span>
      {{else if .BreakStopped}}
        <span class="badge badge-grey"><span class="dot"></span>beendet</span>
      {{else if .WorkStarted}}
        <span class="badge badge-grey"><span class="dot"></span>ausstehend</span>
      {{else}}
        <span class="badge badge-grey"><span class="dot"></span>—</span>
      {{end}}
    </div>

    {{if .WorkComplete}}
    <div class="status-row">
      <span class="label">Tag</span>
      <span class="badge badge-green"><span class="dot"></span>abgeschlossen</span>
    </div>
    {{end}}

    {{end}}{{/* end DayOff else */}}

    {{if .Config.DryRun}}
    <div class="status-row">
      <span class="label">Modus</span>
      <span class="badge badge-amber"><span class="dot"></span>DRY RUN</span>
    </div>
    {{end}}

  </div>
  {{if .DayNote}}
  <p class="status-hint">Notiz: {{.DayNote}}</p>
  {{end}}
</div>

<!-- ── Nettoarbeitszeit ── -->
<div class="card">
  <h2>Nettoarbeitszeit</h2>
  {{if .WorkStarted}}
    <div class="bignum">{{.NetWork}}</div>
    <div class="bignum-label">Heute (Stand {{.Now.Format "15:04:05"}})</div>
    {{if .WorkActive}}<p class="bignum-note">⏳ Arbeit läuft noch</p>{{end}}
    {{if .BreakActive}}<p class="bignum-note">☕ Pause läuft</p>{{end}}
    {{if .WorkComplete}}<p class="bignum-note">✓ Tag abgeschlossen</p>{{end}}
  {{else if .DayOff}}
    <div class="bignum" style="font-size:2rem;color:var(--muted)">—</div>
    <div class="bignum-label">Kein Arbeitstag</div>
  {{else}}
    <div class="bignum" style="font-size:2rem;color:var(--muted)">—</div>
    <div class="bignum-label">Arbeit noch nicht gestartet</div>
  {{end}}
</div>

<!-- ── Manuelle Aktionen ── -->
{{if or .CanStartWork .CanStopWork .CanStartBreak .CanStopBreak}}
<div class="card full">
  <h2>Manuelle Steuerung</h2>
  <div class="actions">
    {{if .CanStartWork}}
    <form method="post" action="/api/action" onsubmit="return confirm('Arbeit jetzt manuell starten?')">
      <input type="hidden" name="action" value="start_work">
      <button class="btn btn-green" type="submit">▶ Arbeit starten</button>
    </form>
    {{end}}
    {{if .CanStartBreak}}
    <form method="post" action="/api/action" onsubmit="return confirm('Pause jetzt manuell starten?')">
      <input type="hidden" name="action" value="start_break">
      <button class="btn btn-amber" type="submit">⏸ Pause starten</button>
    </form>
    {{end}}
    {{if .CanStopBreak}}
    <form method="post" action="/api/action" onsubmit="return confirm('Pause jetzt manuell beenden?')">
      <input type="hidden" name="action" value="stop_break">
      <button class="btn btn-green" type="submit">▶ Pause beenden</button>
    </form>
    {{end}}
    {{if .CanStopWork}}
    <form method="post" action="/api/action" onsubmit="return confirm('Arbeit jetzt manuell beenden?')">
      <input type="hidden" name="action" value="stop_work">
      <button class="btn btn-red" type="submit">⏹ Arbeit beenden</button>
    </form>
    {{end}}
  </div>
</div>
{{end}}

<!-- ── Geplante Zeiten / Timeline ── -->
<div class="card full">
  <h2>Tagesplan</h2>
  {{if or .WorkStarted (ne .PlannedStartWork "—")}}
  <div class="timeline">

    <!-- Arbeitsbeginn -->
    <div class="tl-row">
      <div class="tl-left">
        <div class="tl-dot {{if .WorkStarted}}tl-dot-done{{else}}tl-dot-pending{{end}}"></div>
        <div class="tl-line"></div>
      </div>
      <div class="tl-content">
        <div class="tl-name">Arbeitsbeginn</div>
        <div class="tl-times">
          <div class="tl-time-item">
            <span class="tlabel">Geplant</span>
            <span class="tval">{{.PlannedStartWork}}</span>
          </div>
          {{if .WorkStarted}}
          <div class="tl-time-item">
            <span class="tlabel">Tatsächlich</span>
            <span class="tval">{{.WorkStartTime}}</span>
          </div>
          {{end}}
        </div>
        {{if .WorkStarted}}<div class="tl-status"><span class="badge badge-green" style="padding:2px 8px;font-size:.72rem"><span class="dot"></span>erledigt</span></div>{{end}}
      </div>
    </div>

    <!-- Pausenbeginn -->
    <div class="tl-row">
      <div class="tl-left">
        <div class="tl-dot {{if .BreakStarted}}tl-dot-done{{else if .WorkStarted}}tl-dot-active{{else}}tl-dot-pending{{end}}"></div>
        <div class="tl-line"></div>
      </div>
      <div class="tl-content">
        <div class="tl-name">Pausenbeginn</div>
        <div class="tl-times">
          <div class="tl-time-item">
            <span class="tlabel">Geplant</span>
            <span class="tval">{{.PlannedStartBreak}}</span>
          </div>
          {{if .BreakStarted}}
          <div class="tl-time-item">
            <span class="tlabel">Tatsächlich</span>
            <span class="tval">{{.BreakStartTime}}</span>
          </div>
          {{end}}
        </div>
        {{if .BreakStarted}}<div class="tl-status"><span class="badge badge-green" style="padding:2px 8px;font-size:.72rem"><span class="dot"></span>erledigt</span></div>{{end}}
      </div>
    </div>

    <!-- Pausenende -->
    <div class="tl-row">
      <div class="tl-left">
        <div class="tl-dot {{if .BreakStopped}}tl-dot-done{{else if .BreakActive}}tl-dot-active{{else}}tl-dot-pending{{end}}"></div>
        <div class="tl-line"></div>
      </div>
      <div class="tl-content">
        <div class="tl-name">Pausenende</div>
        <div class="tl-times">
          <div class="tl-time-item">
            <span class="tlabel">Geplant</span>
            <span class="tval">{{.PlannedStopBreak}}</span>
          </div>
          {{if .BreakStopped}}
          <div class="tl-time-item">
            <span class="tlabel">Tatsächlich</span>
            <span class="tval">{{.BreakStopTime}}</span>
          </div>
          {{end}}
        </div>
        {{if .BreakActive}}<div class="tl-status"><span class="badge badge-amber" style="padding:2px 8px;font-size:.72rem"><span class="dot"></span>Pause läuft</span></div>{{end}}
        {{if .BreakStopped}}<div class="tl-status"><span class="badge badge-green" style="padding:2px 8px;font-size:.72rem"><span class="dot"></span>erledigt</span></div>{{end}}
      </div>
    </div>

    <!-- Arbeitsende -->
    <div class="tl-row">
      <div class="tl-left">
        <div class="tl-dot {{if .WorkStopped}}tl-dot-done{{else if and .WorkActive .BreakStopped}}tl-dot-active{{else}}tl-dot-pending{{end}}"></div>
        <div class="tl-line"></div>
      </div>
      <div class="tl-content">
        <div class="tl-name">Arbeitsende</div>
        <div class="tl-times">
          <div class="tl-time-item">
            <span class="tlabel">Geplant</span>
            <span class="tval">{{.PlannedStopWork}}</span>
          </div>
          {{if .WorkStopped}}
          <div class="tl-time-item">
            <span class="tlabel">Tatsächlich</span>
            <span class="tval">{{.WorkStopTime}}</span>
          </div>
          {{end}}
        </div>
        {{if .WorkStopped}}<div class="tl-status"><span class="badge badge-green" style="padding:2px 8px;font-size:.72rem"><span class="dot"></span>erledigt</span></div>{{end}}
      </div>
    </div>

  </div>
  {{else}}
  <p style="font-size:.875rem;color:var(--muted)">Geplante Zeiten werden beim ersten Arbeitstag-Tick gesetzt.</p>
  {{end}}
</div>

<!-- ── Verlauf (letzte 7 Tage) ── -->
{{if .History}}
<div class="card full">
  <h2>Verlauf</h2>
  <table class="hist-table">
    <thead>
      <tr>
        <th>Datum</th>
        <th>Arbeitsbeginn</th>
        <th>Arbeitsende</th>
        <th>Nettozeit</th>
        <th>Status</th>
      </tr>
    </thead>
    <tbody>
      {{range .History}}
      <tr>
        <td class="hist-date">{{.Date}}</td>
        <td>{{.WorkStart}}</td>
        <td>{{.WorkEnd}}</td>
        <td class="hist-net">{{.NetWork}}</td>
        <td>{{.Status}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</div>
{{end}}

<!-- ── Einstellungen ── -->
<div class="card full">
  <h2>Einstellungen</h2>
  <div class="cfg-grid">
    <div>
      <div class="cfg-row"><span class="k">Endpunkt</span><span class="v">{{.Config.Endpoint}}</span></div>
      <div class="cfg-row"><span class="k">Benutzer</span><span class="v">{{.Config.Username}}</span></div>
      <div class="cfg-row"><span class="k">Aufgabe</span><span class="v">{{if .Config.Task}}{{.Config.Task}}{{else}}—{{end}}</span></div>
      <div class="cfg-row"><span class="k">Arbeitstage</span><span class="v">{{if .Config.WorkDays}}{{.Config.WorkDays}}{{else}}täglich{{end}}</span></div>
      <div class="cfg-row"><span class="k">Arbeitsbeginn</span><span class="v">{{.Config.StartWorkMin}} – {{.Config.StartWorkMax}}</span></div>
      <div class="cfg-row"><span class="k">Pausenbeginn</span><span class="v">{{.Config.StartBreakMin}} – {{.Config.StartBreakMax}}</span></div>
    </div>
    <div>
      <div class="cfg-row"><span class="k">Arbeitszeit</span><span class="v">{{.Config.MinWorkDuration}} – {{.Config.MaxWorkDuration}}</span></div>
      <div class="cfg-row"><span class="k">Pausendauer</span><span class="v">{{.Config.MinBreakDuration}} – {{.Config.MaxBreakDuration}}</span></div>
      <div class="cfg-row"><span class="k">State-Datei</span><span class="v">{{.Config.StateFile}}</span></div>
      <div class="cfg-row"><span class="k">ICS-Cache</span><span class="v">{{.Config.ICSCacheDir}}</span></div>
      <div class="cfg-row"><span class="k">Discord Webhook</span><span class="v">{{if .Config.WebhookSet}}✓ konfiguriert{{else}}—{{end}}</span></div>
      <div class="cfg-row"><span class="k">Feiertagskalender</span><span class="v">{{if .Config.HolidaySet}}✓ konfiguriert{{else}}—{{end}}</span></div>
      <div class="cfg-row"><span class="k">Urlaubskalender</span><span class="v">{{if .Config.VacationSet}}✓ konfiguriert{{else}}—{{end}}</span></div>
    </div>
  </div>
  {{if or .Config.DryRun .Config.Verbose}}
  <div class="flags" style="margin-top:14px;padding-top:12px;border-top:1px solid var(--border)">
    {{if .Config.DryRun}}<span class="badge badge-amber"><span class="dot"></span>DRY RUN aktiv</span>{{end}}
    {{if .Config.Verbose}}<span class="badge badge-grey"><span class="dot"></span>Verbose aktiv</span>{{end}}
  </div>
  {{end}}
</div>

</main>
</body>
</html>`
