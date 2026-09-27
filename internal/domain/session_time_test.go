package domain

import (
	"testing"
	"time"
)

func TestDurationExcludesPausedDays(t *testing.T) {
	start := time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)
	session := ScoreSession{Status: "active", CreatedAt: start, RunningSince: &start}
	if got := session.DurationSeconds(start.Add(90 * time.Minute)); got != 5400 {
		t.Fatalf("playing duration: %d", got)
	}
	session.Pause(start.Add(90 * time.Minute))
	if got := session.DurationSeconds(start.Add(72 * time.Hour)); got != 5400 {
		t.Fatalf("paused days were counted: %d", got)
	}
	session.Resume(start.Add(72 * time.Hour))
	session.Finish(start.Add(72*time.Hour + 30*time.Minute))
	if got := session.DurationSeconds(start.Add(10 * 24 * time.Hour)); got != 7200 {
		t.Fatalf("finished duration: %d", got)
	}
}
