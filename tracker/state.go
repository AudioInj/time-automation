// tracker/state.go
package tracker

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type DayState struct {
	WorkStarted    bool      `json:"work_started"`
	WorkStartTime  time.Time `json:"work_start_time"`
	WorkStopped    bool      `json:"work_stopped"`
	BreakStarted   bool      `json:"break_started"`
	BreakStartTime time.Time `json:"break_start_time"`
	BreakStopped   bool      `json:"break_stopped"`
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

// NetWorkDuration returns the net working time for the day (work duration minus break).
// For in-progress periods, the current time is used as the end.
// Uses PlannedStopWork / PlannedStopBreak as end times when the day is completed.
func (ds DayState) NetWorkDuration() time.Duration {
	if !ds.WorkStarted {
		return 0
	}
	now := time.Now()

	workEnd := now
	if ds.WorkStopped && !ds.PlannedStopWork.IsZero() {
		workEnd = ds.PlannedStopWork
	}
	work := workEnd.Sub(ds.WorkStartTime)

	var brk time.Duration
	if ds.BreakStarted {
		breakEnd := now
		if ds.BreakStopped && !ds.PlannedStopBreak.IsZero() {
			breakEnd = ds.PlannedStopBreak
		}
		brk = breakEnd.Sub(ds.BreakStartTime)
	}
	if brk > work {
		brk = work
	}
	return work - brk
}

type StateTracker struct {
	path string
	lock sync.Mutex
	data map[string]DayState
}

func New(path string) *StateTracker {
	return &StateTracker{
		path: path,
		data: make(map[string]DayState),
	}
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
	s.loadFile()

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

func (s *StateTracker) loadFile() {
	file, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer file.Close()
	content, _ := io.ReadAll(file)
	json.Unmarshal(content, &s.data)
}

func (s *StateTracker) saveFile() {
	file, err := os.Create(s.path)
	if err != nil {
		log.Printf("[STATE] Failed to open state file for writing: %v", err)
		return
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.data); err != nil {
		log.Printf("[STATE] Failed to encode state: %v", err)
	}
	if err := file.Sync(); err != nil {
		log.Printf("[STATE] Failed to sync state file: %v", err)
	}
}
