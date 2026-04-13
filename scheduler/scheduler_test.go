package scheduler

import (
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// isWorkDay
// ---------------------------------------------------------------------------

func TestIsWorkDay(t *testing.T) {
	// time.Weekday: Sunday=0, Monday=1, ..., Saturday=6
	monday := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)    // 1
	wednesday := time.Date(2024, 1, 10, 9, 0, 0, 0, time.UTC) // 3
	saturday := time.Date(2024, 1, 13, 9, 0, 0, 0, time.UTC)  // 6
	sunday := time.Date(2024, 1, 7, 9, 0, 0, 0, time.UTC)     // 0

	tests := []struct {
		name     string
		workDays string
		day      time.Time
		want     bool
	}{
		{"monday in 1-5", "1-5", monday, true},
		{"wednesday in 1-5", "1-5", wednesday, true},
		{"saturday not in 1-5", "1-5", saturday, false},
		{"sunday not in 1-5", "1-5", sunday, false},
		{"saturday in 0,6", "0,6", saturday, true},
		{"sunday in 0,6", "0,6", sunday, true},
		{"monday not in 0,6", "0,6", monday, false},
		{"monday in 1,3,5", "1,3,5", monday, true},
		{"wednesday in 1,3,5", "1,3,5", wednesday, true},
		{"saturday not in 1,3,5", "1,3,5", saturday, false},
		{"empty means all days (monday)", "", monday, true},
		{"empty means all days (saturday)", "", saturday, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWorkDay(tt.workDays, tt.day)
			if got != tt.want {
				t.Errorf("isWorkDay(%q, %v) = %v, want %v",
					tt.workDays, tt.day.Weekday(), got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// randomizeTimeRange
// ---------------------------------------------------------------------------

func TestRandomizeTimeRangeWithinBounds(t *testing.T) {
	base := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	// max includes up to 59 extra seconds (the function randomises seconds too)
	earliest := time.Date(2024, 1, 8, 8, 0, 0, 0, time.UTC)
	latest := time.Date(2024, 1, 8, 9, 30, 59, 0, time.UTC)

	for i := 0; i < 200; i++ {
		result := randomizeTimeRange(base, "08:00", "09:30")
		if result.Before(earliest) || result.After(latest) {
			t.Errorf("result %v out of expected range [%v, %v]", result, earliest, latest)
		}
	}
}

func TestRandomizeTimeRangeEqualBounds(t *testing.T) {
	base := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	result := randomizeTimeRange(base, "09:00", "09:00")
	if result.Hour() != 9 || result.Minute() != 0 {
		t.Errorf("expected 09:00, got %v", result.Format("15:04"))
	}
}

// ---------------------------------------------------------------------------
// ICS helpers
// ---------------------------------------------------------------------------

const sampleHolidayICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//Test//EN
BEGIN:VEVENT
DTSTART;VALUE=DATE:20240108
DTEND;VALUE=DATE:20240109
SUMMARY:New Year's Day
END:VEVENT
BEGIN:VEVENT
DTSTART;VALUE=DATE:20240201
DTEND;VALUE=DATE:20240202
SUMMARY:Carnival
END:VEVENT
END:VCALENDAR`

const sampleVacationICS = `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART;VALUE=DATE:20240108
SUMMARY:Urlaub München
END:VEVENT
END:VCALENDAR`

func writeTempICS(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "test-*.ics")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(content)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestGetICSTodaySummary(t *testing.T) {
	path := writeTempICS(t, sampleHolidayICS)

	tests := []struct {
		day  time.Time
		want string
	}{
		{time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC), "New Year's Day"},
		{time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), "Carnival"},
		{time.Date(2024, 1, 9, 0, 0, 0, 0, time.UTC), ""},
	}
	for _, tt := range tests {
		got := getICSTodaySummary(path, tt.day)
		if got != tt.want {
			t.Errorf("getICSTodaySummary(%s) = %q, want %q",
				tt.day.Format("20060102"), got, tt.want)
		}
	}
}

func TestGetICSTodaySummaryMissingFile(t *testing.T) {
	got := getICSTodaySummary("/nonexistent/path.ics", time.Now())
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func TestIsICSToday(t *testing.T) {
	path := writeTempICS(t, sampleVacationICS)
	match := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	noMatch := time.Date(2024, 1, 9, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		day     time.Time
		keyword string
		want    bool
	}{
		{"match no keyword", match, "", true},
		{"match exact keyword", match, "Urlaub", true},
		{"match case-insensitive keyword", match, "urlaub", true},
		{"no match wrong keyword", match, "NonExistent", false},
		{"no match wrong date", noMatch, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isICSToday(path, tt.day, tt.keyword)
			if got != tt.want {
				t.Errorf("isICSToday(%s, %q) = %v, want %v",
					tt.day.Format("20060102"), tt.keyword, got, tt.want)
			}
		})
	}
}

func TestIsICSTodayMissingFile(t *testing.T) {
	if isICSToday("/nonexistent/path.ics", time.Now(), "") {
		t.Error("expected false for missing file")
	}
}
