package tracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempStateFile(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "state-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// ---------------------------------------------------------------------------
// Load / Save
// ---------------------------------------------------------------------------

func TestSaveAndLoad(t *testing.T) {
	path := tempStateFile(t)
	tr := New(path)
	date := "2024-01-08"
	workStart := time.Date(2024, 1, 8, 8, 0, 0, 0, time.UTC)

	tr.Save(date, DayState{
		WorkStarted:   true,
		WorkStartTime: workStart,
	})

	loaded := tr.Load(date)
	if !loaded.WorkStarted {
		t.Error("WorkStarted not persisted")
	}
	if !loaded.WorkStartTime.Equal(workStart) {
		t.Errorf("WorkStartTime mismatch: got %v, want %v", loaded.WorkStartTime, workStart)
	}
}

func TestLoadMissingDate(t *testing.T) {
	loaded := New(tempStateFile(t)).Load("1999-01-01")
	if loaded.WorkStarted || loaded.BreakStarted || loaded.IsHoliday || loaded.IsVacation {
		t.Error("expected zero-value DayState for unknown date")
	}
}

// ---------------------------------------------------------------------------
// Persistence across instances (exercises the json tags on IsHoliday/IsVacation)
// ---------------------------------------------------------------------------

func TestPersistenceAcrossInstances(t *testing.T) {
	path := tempStateFile(t)
	date := "2024-01-08"

	New(path).Save(date, DayState{
		WorkStarted: true,
		IsHoliday:   true,
		IsVacation:  false,
	})

	loaded := New(path).Load(date)
	if !loaded.WorkStarted {
		t.Error("WorkStarted not persisted across instances")
	}
	if !loaded.IsHoliday {
		t.Error("IsHoliday not persisted across instances (missing json tag?)")
	}
	if loaded.IsVacation {
		t.Error("IsVacation should be false")
	}
}

func TestVacationPersistence(t *testing.T) {
	path := tempStateFile(t)
	date := "2024-01-09"

	New(path).Save(date, DayState{IsVacation: true})

	loaded := New(path).Load(date)
	if !loaded.IsVacation {
		t.Error("IsVacation not persisted across instances (missing json tag?)")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

func TestReset(t *testing.T) {
	path := tempStateFile(t)
	date := "2024-01-08"
	tr := New(path)

	tr.Save(date, DayState{WorkStarted: true, IsHoliday: true})
	tr.Reset(date)

	loaded := tr.Load(date)
	if loaded.WorkStarted || loaded.IsHoliday {
		t.Error("state not cleared after Reset")
	}
}

// ---------------------------------------------------------------------------
// NetWorkDuration
// ---------------------------------------------------------------------------

func TestNetWorkDurationFullDay(t *testing.T) {
	// 8:00 – 17:00 work, 12:00 – 12:45 break → net = 8h15m
	ds := DayState{
		WorkStarted:      true,
		WorkStartTime:    time.Date(2024, 1, 8, 8, 0, 0, 0, time.UTC),
		WorkStopped:      true,
		PlannedStopWork:  time.Date(2024, 1, 8, 17, 0, 0, 0, time.UTC),
		BreakStarted:     true,
		BreakStartTime:   time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC),
		BreakStopped:     true,
		PlannedStopBreak: time.Date(2024, 1, 8, 12, 45, 0, 0, time.UTC),
	}
	got := ds.NetWorkDuration()
	want := 8*time.Hour + 15*time.Minute
	if got != want {
		t.Errorf("NetWorkDuration() = %v, want %v", got, want)
	}
}

func TestNetWorkDurationNoWork(t *testing.T) {
	ds := DayState{}
	if ds.NetWorkDuration() != 0 {
		t.Error("expected 0 for day with no work started")
	}
}

func TestNetWorkDurationNoBreak(t *testing.T) {
	// 8:00 – 16:30, no break → net = 8h30m
	ds := DayState{
		WorkStarted:     true,
		WorkStartTime:   time.Date(2024, 1, 8, 8, 0, 0, 0, time.UTC),
		WorkStopped:     true,
		PlannedStopWork: time.Date(2024, 1, 8, 16, 30, 0, 0, time.UTC),
	}
	got := ds.NetWorkDuration()
	want := 8*time.Hour + 30*time.Minute
	if got != want {
		t.Errorf("NetWorkDuration() = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Corrupted state file
// ---------------------------------------------------------------------------

func TestCorruptedStateFileIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	// Write deliberately broken JSON
	if err := os.WriteFile(path, []byte("{corrupted json!!!"), 0644); err != nil {
		t.Fatal(err)
	}

	// New() should not panic; the corrupt file is silently discarded.
	tr := New(path)

	// All dates must return an empty state.
	got := tr.Load("2024-01-01")
	if got.WorkStarted || got.IsHoliday || got.IsVacation {
		t.Errorf("expected zero-value state after corrupt load, got %+v", got)
	}

	// Normal save/load must work after a corrupt start.
	tr.Save("2024-01-01", DayState{WorkStarted: true})
	got = tr.Load("2024-01-01")
	if !got.WorkStarted {
		t.Error("expected WorkStarted=true after save following corrupt load")
	}
}
