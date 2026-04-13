// scheduler/scheduler.go
package scheduler

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/audioinj/time-automation/config"
	"github.com/audioinj/time-automation/executor"
	"github.com/audioinj/time-automation/notify"
	"github.com/audioinj/time-automation/tracker"
)

type Scheduler struct {
	cfg      config.Config
	state    *tracker.StateTracker
	executor *executor.Executor
	notify   *notify.Notifier

	randomizedTimes map[string]time.Time
	randomizedDay   int

	holidayCheckedDay  int
	holidayCheckedType string

	calendarFetchedDay int // Track last day calendars were fetched
	httpClient         *http.Client
}

func New(cfg config.Config, s *tracker.StateTracker, e *executor.Executor, n *notify.Notifier) *Scheduler {
	if cfg.ICSCacheDir != "" {
		if err := os.MkdirAll(cfg.ICSCacheDir, 0750); err != nil {
			log.Printf("[SCHEDULER] Failed to create ICS cache dir %s: %v", cfg.ICSCacheDir, err)
		}
	}
	return &Scheduler{
		cfg:             cfg,
		state:           s,
		executor:        e,
		notify:          n,
		randomizedTimes: make(map[string]time.Time),
		randomizedDay:   -1,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Randomize a time between min and max (HH:MM), including random seconds
func randomizeTimeRange(base time.Time, minStr, maxStr string) time.Time {
	minT, _ := time.Parse("15:04", minStr)
	maxT, _ := time.Parse("15:04", maxStr)
	min := time.Date(base.Year(), base.Month(), base.Day(), minT.Hour(), minT.Minute(), 0, 0, base.Location())
	max := time.Date(base.Year(), base.Month(), base.Day(), maxT.Hour(), maxT.Minute(), 59, 0, base.Location())
	span := max.Sub(min)
	if span <= 0 {
		return min
	}
	sec := rand.Int63n(int64(span.Seconds()) + 1)
	return min.Add(time.Duration(sec) * time.Second)
}

func randomDuration(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int63n(int64(max-min+1)))
}

// Call this once per day to randomize all times and print them
func (s *Scheduler) randomizeAllTimes(ctx context.Context, now time.Time) {
	today := now.Format("2006-01-02")
	st := s.state.Load(today)

	// Use planned times from state if present, otherwise randomize and persist
	var startWork, startBreak, stopBreak, stopWork time.Time

	if !st.PlannedStartWork.IsZero() {
		startWork = st.PlannedStartWork
	} else if st.WorkStarted && !st.WorkStartTime.IsZero() {
		startWork = st.WorkStartTime
		st.PlannedStartWork = startWork
	} else {
		startWork = randomizeTimeRange(now, s.cfg.StartWorkMin.Format("15:04"), s.cfg.StartWorkMax.Format("15:04"))
		st.PlannedStartWork = startWork
	}

	if !st.PlannedStartBreak.IsZero() {
		startBreak = st.PlannedStartBreak
	} else if st.BreakStarted && !st.BreakStartTime.IsZero() {
		startBreak = st.BreakStartTime
		st.PlannedStartBreak = startBreak
	} else {
		startBreak = randomizeTimeRange(now, s.cfg.StartBreakMin.Format("15:04"), s.cfg.StartBreakMax.Format("15:04"))
		st.PlannedStartBreak = startBreak
	}

	if !st.PlannedStopBreak.IsZero() {
		stopBreak = st.PlannedStopBreak
	} else {
		minBreak := s.cfg.MinBreakDuration
		maxBreak := s.cfg.MaxBreakDuration
		breakDuration := randomDuration(minBreak, maxBreak)
		stopBreak = startBreak.Add(breakDuration)
		st.PlannedStopBreak = stopBreak
	}

	if !st.PlannedStopWork.IsZero() {
		stopWork = st.PlannedStopWork
	} else {
		minWork := s.cfg.MinWorkDuration
		maxWork := s.cfg.MaxWorkDuration
		workDuration := randomDuration(minWork, maxWork)
		minBreak := s.cfg.MinBreakDuration
		maxBreak := s.cfg.MaxBreakDuration
		breakDuration := stopBreak.Sub(startBreak)
		// If break duration is not plausible, randomize
		if breakDuration <= 0 {
			breakDuration = randomDuration(minBreak, maxBreak)
		}
		stopWork = startWork.Add(workDuration + breakDuration)
		st.PlannedStopWork = stopWork
	}

	// Save planned times into state only if they were missing
	s.state.Save(today, st)

	s.randomizedTimes = map[string]time.Time{
		"START_WORK":  startWork,
		"START_BREAK": startBreak,
		"STOP_BREAK":  stopBreak,
		"STOP_WORK":   stopWork,
	}
	s.randomizedDay = now.YearDay()

	// Prepare and send planned times notification (log only once)
	plannedMsg := fmt.Sprintf(
		"START_WORK:  %s\nSTART_BREAK: %s\nSTOP_BREAK:  %s\nSTOP_WORK:   %s",
		startWork.Format("15:04:05"),
		startBreak.Format("15:04:05"),
		stopBreak.Format("15:04:05"),
		stopWork.Format("15:04:05"),
	)
	log.Println("[PLANNED TIMES]\n" + plannedMsg)
}

func (s *Scheduler) fetchCalendar(ctx context.Context, url, cachePath string, useAuth bool) ([]byte, error) {
	if url == "" {
		return nil, nil
	}
	data, err := s.fetchCalendarRemote(ctx, url, cachePath, useAuth)
	if err == nil {
		return data, nil
	}
	log.Printf("[CALENDAR] Failed to fetch from %s, using cache if available", url)
	cached, cacheErr := os.ReadFile(cachePath)
	if cacheErr == nil {
		log.Printf("[CALENDAR] Using cached for %s", url)
		return cached, nil
	}
	log.Printf("[CALENDAR] Not available for %s", url)
	return nil, err
}

// fetchCalendarRemote performs the actual HTTP request and caches the result on success.
func (s *Scheduler) fetchCalendarRemote(ctx context.Context, url, cachePath string, useAuth bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("[CALENDAR] Failed to create request for %s: %v", url, err)
		return nil, err
	}
	if useAuth {
		req.SetBasicAuth(s.cfg.Username+"@"+s.cfg.Domain, s.cfg.Password)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[CALENDAR] Failed to fetch from %s: %v", url, err)
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		log.Printf("[CALENDAR] Fetch returned %s for %s", resp.Status, url)
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		log.Printf("[CALENDAR] Failed to cache to %s: %v", cachePath, err)
	}
	log.Printf("[CALENDAR] Fetched and cached: %s", url)
	return data, nil
}

func (s *Scheduler) isTodayHolidayOrVacation(ctx context.Context, now time.Time) (bool, string, string) {
	holidayPath := filepath.Join(s.cfg.ICSCacheDir, "holiday.ics")
	vacationPath := filepath.Join(s.cfg.ICSCacheDir, "vacation.ics")

	// Fetch calendars only once per day
	if s.calendarFetchedDay != now.YearDay() {
		s.calendarFetchedDay = now.YearDay()
		log.Println("[CALENDAR] Fetching calendars...")
		if s.cfg.HolidayAddress != "" {
			_, _ = s.fetchCalendar(ctx, s.cfg.HolidayAddress, holidayPath, false)
		}
		if s.cfg.VacationAddress != "" {
			_, _ = s.fetchCalendar(ctx, s.cfg.VacationAddress, vacationPath, true)
		}
	}
	// Try to extract holiday name if present
	holidayName := ""
	if s.cfg.HolidayAddress != "" {
		holidayName = getICSTodaySummary(holidayPath, now)
		if holidayName != "" {
			log.Printf("[SCHEDULER] Today is a public holiday (%s): %s", now.Format("2006-01-02"), holidayName)
			return true, "Public holiday", holidayName
		}
	}
	if s.cfg.VacationAddress != "" && isICSToday(vacationPath, now, s.cfg.VacationKeyword) {
		log.Printf("[SCHEDULER] Today is a vacation day (%s)", now.Format("2006-01-02"))
		return true, "Vacation", ""
	}
	return false, "", ""
}

// Helper: get the SUMMARY for today's event in an ICS file.
// Evaluates at END:VEVENT so property order within the block does not matter.
func getICSTodaySummary(path string, now time.Time) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	dateStr := now.Format("20060102")
	inEvent := false
	summary := ""
	hasDate := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "BEGIN:VEVENT" {
			inEvent = true
			summary = ""
			hasDate = false
		}
		if inEvent && strings.HasPrefix(line, "SUMMARY:") {
			summary = strings.TrimPrefix(line, "SUMMARY:")
		}
		if inEvent && strings.HasPrefix(line, "DTSTART") && strings.Contains(line, dateStr) {
			hasDate = true
		}
		if line == "END:VEVENT" {
			if inEvent && hasDate {
				return summary
			}
			inEvent = false
		}
	}
	return ""
}

// isICSToday reports whether today is represented by an event in the ICS file,
// optionally requiring the SUMMARY to contain keyword (case-insensitive).
// Evaluates at END:VEVENT so property order within the block does not matter.
func isICSToday(path string, now time.Time, keyword string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	dateStr := now.Format("20060102")
	inEvent := false
	hasKeyword := keyword == ""
	hasDate := false
	keywordLower := strings.ToLower(keyword)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "BEGIN:VEVENT" {
			inEvent = true
			hasKeyword = keyword == ""
			hasDate = false
		}
		if inEvent && strings.HasPrefix(line, "SUMMARY:") && keyword != "" {
			summary := strings.TrimPrefix(line, "SUMMARY:")
			if strings.Contains(strings.ToLower(summary), keywordLower) {
				hasKeyword = true
			}
		}
		if inEvent && strings.HasPrefix(line, "DTSTART") && strings.Contains(line, dateStr) {
			hasDate = true
		}
		if line == "END:VEVENT" {
			if inEvent && hasDate && hasKeyword {
				return true
			}
			inEvent = false
		}
	}
	return false
}

func (s *Scheduler) verboseLog(msg string) {
	if s.executor != nil {
		s.executor.VerboseLog(msg)
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	now := time.Now()

	// Only check for holiday/vacation if today is a workday
	if isWorkDay(s.cfg.WorkDays, now) {
		today := now.Format("2006-01-02")
		st := s.state.Load(today)

		// Only check and notify if not already marked as holiday/vacation in state and not already checked in memory
		if !st.IsHoliday && !st.IsVacation && s.holidayCheckedDay != now.YearDay() {
			if isHoliday, reason, holidayName := s.isTodayHolidayOrVacation(ctx, now); isHoliday {
				var msg string
				checkedType := ""
				switch reason {
				case "Public holiday":
					if holidayName != "" {
						msg = fmt.Sprintf("%s\n%s", holidayName, now.Format("2006-01-02"))
					} else {
						msg = fmt.Sprintf("Public holiday\n%s", now.Format("2006-01-02"))
					}
					st.IsHoliday = true
					checkedType = "holiday"
				case "Vacation":
					msg = fmt.Sprintf("Vacation\n%s", now.Format("2006-01-02"))
					st.IsVacation = true
					checkedType = "vacation"
				default:
					msg = fmt.Sprintf("No work today: %s (%s)", reason, now.Format("2006-01-02"))
					checkedType = "other"
				}
				log.Println("[SCHEDULER]", msg)
				if s.notify != nil && s.cfg.WebhookURL != "" {
					s.notify.Send(ctx, reason, msg)
				}
				st.DayNote = reason
				s.state.Save(today, st)
				s.holidayCheckedDay = now.YearDay()
				s.holidayCheckedType = checkedType
				return
			}
		} else if st.IsHoliday || st.IsVacation || s.holidayCheckedDay == now.YearDay() {
			// Already marked or checked, skip further actions for today
			return
		}
	} else {
		// Not a workday, do nothing (no notification, no state update)
		return
	}

	// Randomize times once per day (at midnight or first run of the day)
	if s.randomizedDay != now.YearDay() {
		s.randomizeAllTimes(ctx, now)
	}

	today := now.Format("2006-01-02")
	st := s.state.Load(today)

	// --- Prevent any further actions if the full cycle is done for the day ---
	if st.WorkStarted && st.WorkStopped && st.BreakStarted && st.BreakStopped {
		// All actions for the day are done, do nothing more
		return
	}

	if now.After(s.randomizedTimes["START_WORK"]) && !st.WorkStarted {
		s.executor.StartWork(ctx)
		st.WorkStarted = true
		st.WorkStartTime = time.Now()
		s.state.Save(today, st)
		log.Printf("[SCHEDULER] start_work at %s (planned %s)",
			now.Format("15:04:05"), s.randomizedTimes["START_WORK"].Format("15:04:05"))
		if s.notify != nil {
			s.notify.Send(ctx, "▶️ Arbeit gestartet", now.Format("15:04:05"))
		}
		return
	}

	if st.WorkStarted && !st.BreakStarted && !st.BreakStopped && now.After(s.randomizedTimes["START_BREAK"]) {
		s.executor.StartBreak(ctx)
		st.BreakStarted = true
		st.BreakStartTime = time.Now()
		s.state.Save(today, st)
		log.Printf("[SCHEDULER] start_break at %s (planned %s)",
			now.Format("15:04:05"), s.randomizedTimes["START_BREAK"].Format("15:04:05"))
		if s.notify != nil {
			s.notify.Send(ctx, "⏸️ Pause gestartet", now.Format("15:04:05"))
		}
		return
	}

	if st.BreakStarted && !st.BreakStopped {
		plannedStopBreak := s.randomizedTimes["STOP_BREAK"]
		minBreakMet := time.Since(st.BreakStartTime) >= s.cfg.MinBreakDuration
		afterPlannedStop := now.After(plannedStopBreak)
		if minBreakMet && afterPlannedStop {
			s.executor.StopBreak(ctx)
			st.BreakStopped = true
			st.BreakStopTime = time.Now()
			s.state.Save(today, st)
			log.Printf("[SCHEDULER] stop_break at %s (planned %s)",
				now.Format("15:04:05"), plannedStopBreak.Format("15:04:05"))
			if s.notify != nil {
				s.notify.Send(ctx, "▶️ Pause beendet", now.Format("15:04:05"))
			}
		} else if !minBreakMet {
			s.verboseLog("[INFO] Not stopping break: minimum duration not met.")
		} else if !afterPlannedStop {
			s.verboseLog("[INFO] Not stopping break: planned stop time not reached.")
		}
		return
	}

	if st.WorkStarted && !st.WorkStopped && now.After(s.randomizedTimes["STOP_WORK"]) {
		if time.Since(st.WorkStartTime) >= s.cfg.MinWorkDuration {
			s.executor.StopWork(ctx)
			st.WorkStopped = true
			st.WorkStopTime = time.Now()
			s.state.Save(today, st)
			net := st.NetWorkDuration()
			log.Printf("[SCHEDULER] stop_work at %s (planned %s, net %s)",
				now.Format("15:04:05"), s.randomizedTimes["STOP_WORK"].Format("15:04:05"), formatDuration(net))
			if s.notify != nil {
				s.notify.Send(ctx, "⏹️ Arbeit beendet",
					fmt.Sprintf("%s  ·  Nettozeit: %s", now.Format("15:04:05"), formatDuration(net)))
			}
			// Do not reset state here; keep the day's state for metrics and to prevent re-triggering
		} else {
			s.verboseLog("[INFO] Not stopping work: minimum duration not met.")
		}
		return
	}
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0h 00m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %02dm", h, m)
}

func isWorkDay(workDays string, now time.Time) bool {
	// workDays: e.g. "1-5", "1,3,5", "0,6"
	if workDays == "" {
		return true // default: run every day
	}
	weekday := int(now.Weekday()) // Sunday=0, Monday=1, ..., Saturday=6
	for _, part := range strings.Split(workDays, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) == 2 {
				start, _ := strconv.Atoi(bounds[0])
				end, _ := strconv.Atoi(bounds[1])
				if start <= weekday && weekday <= end {
					return true
				}
			}
		} else {
			val, _ := strconv.Atoi(part)
			if val == weekday {
				return true
			}
		}
	}
	return false
}
