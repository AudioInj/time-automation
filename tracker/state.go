// tracker/state.go
package tracker

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DayState struct {
	WorkStarted    bool      `json:"work_started"`
	WorkStartTime  time.Time `json:"work_start_time"`
	WorkStopped    bool      `json:"work_stopped"`
	WorkStopTime   time.Time `json:"work_stop_time,omitempty"`
	BreakStarted   bool      `json:"break_started"`
	BreakStartTime time.Time `json:"break_start_time"`
	BreakStopped   bool      `json:"break_stopped"`
	BreakStopTime  time.Time `json:"break_stop_time,omitempty"`
	// Planned times for this day
	PlannedStartWork  time.Time `json:"planned_start_work,omitempty"`
	PlannedStartBreak time.Time `json:"planned_start_break,omitempty"`
	PlannedStopBreak  time.Time `json:"planned_stop_break,omitempty"`
	PlannedStopWork   time.Time `json:"planned_stop_work,omitempty"`
	DayNote           string    `json:"day_note,omitempty"`
	// Additional flags
	IsHoliday  bool `json:"is_holiday,omitempty"`
	IsVacation bool `json:"is_vacation,omitempty"`
}

// NetWorkDuration returns the net working time for the day (time before break + time after break).
// For in-progress periods, the current time is used as the end.
// Actual stop times (WorkStopTime, BreakStopTime) take precedence; PlannedStop* are used as fallback
// for backwards compatibility with state files that predate the actual-stop-time fields.
func (ds DayState) NetWorkDuration() time.Duration {
	if !ds.WorkStarted {
		return 0
	}
	now := time.Now()

	// Determine effective work end
	workEnd := now
	if !ds.WorkStopTime.IsZero() {
		workEnd = ds.WorkStopTime
	} else if ds.WorkStopped && !ds.PlannedStopWork.IsZero() {
		workEnd = ds.PlannedStopWork
	}

	if !ds.BreakStarted {
		// No break at all — full span from work start to work end
		d := workEnd.Sub(ds.WorkStartTime)
		if d < 0 {
			return 0
		}
		return d
	}

	// Part 1: work before break
	part1 := ds.BreakStartTime.Sub(ds.WorkStartTime)
	if part1 < 0 {
		part1 = 0
	}

	if !ds.BreakStopped {
		// Break still in progress — only count work done before break
		return part1
	}

	// Determine effective break end
	breakEnd := now
	if !ds.BreakStopTime.IsZero() {
		breakEnd = ds.BreakStopTime
	} else if !ds.PlannedStopBreak.IsZero() {
		breakEnd = ds.PlannedStopBreak
	}

	// Part 2: work after break
	part2 := workEnd.Sub(breakEnd)
	if part2 < 0 {
		part2 = 0
	}

	return part1 + part2
}

type StateTracker struct {
	path string
	lock sync.Mutex
	data map[string]DayState
}

func New(path string) *StateTracker {
	t := &StateTracker{
		path: path,
		data: make(map[string]DayState),
	}
	t.loadFile() // load persisted state once at startup
	return t
}

func (s *StateTracker) Reset(date string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[date] = DayState{}
	s.saveFile()
}

func (s *StateTracker) Load(date string) DayState {
	s.lock.Lock()
	defer s.lock.Unlock()
	if state, ok := s.data[date]; ok {
		return state
	}
	return DayState{}
}

func (s *StateTracker) Save(date string, state DayState) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[date] = state
	s.saveFile()
}

// LoadAll returns a snapshot of all persisted day states (safe for concurrent use).
func (s *StateTracker) LoadAll() map[string]DayState {
	s.lock.Lock()
	defer s.lock.Unlock()
	result := make(map[string]DayState, len(s.data))
	for k, v := range s.data {
		result[k] = v
	}
	return result
}

func (s *StateTracker) loadFile() {
	file, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer file.Close() //nolint:errcheck
	content, _ := io.ReadAll(file)
	if err := json.Unmarshal(content, &s.data); err != nil {
		log.Printf("[STATE] Failed to parse state file: %v", err)
	}
}

func (s *StateTracker) saveFile() {
	// Write to a temp file in the same directory, then rename atomically.
	// This prevents a partial write from corrupting the state on crash.
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		log.Printf("[STATE] Failed to create temp file: %v", err)
		return
	}
	tmpName := tmp.Name()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.data); err != nil {
		log.Printf("[STATE] Failed to encode state: %v", err)
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Sync(); err != nil {
		log.Printf("[STATE] Failed to sync state: %v", err)
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		log.Printf("[STATE] Failed to close temp file: %v", err)
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		log.Printf("[STATE] Failed to commit state file: %v", err)
		_ = os.Remove(tmpName)
	}
}
