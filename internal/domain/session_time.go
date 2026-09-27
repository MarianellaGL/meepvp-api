package domain

import "time"

// DurationSeconds returns playing time only. Time spent paused is excluded.
func (s ScoreSession) DurationSeconds(now time.Time) int64 {
	seconds := s.PlayedSeconds
	if s.Status == "active" {
		started := s.CreatedAt
		if s.RunningSince != nil {
			started = *s.RunningSince
		}
		if now.After(started) {
			seconds += int64(now.Sub(started).Seconds())
		}
	}
	return seconds
}

func (s *ScoreSession) Pause(now time.Time) {
	s.PlayedSeconds = s.DurationSeconds(now)
	s.RunningSince = nil
	s.PausedAt = &now
	s.Status = "paused"
	s.LastModified = now
}

func (s *ScoreSession) Resume(now time.Time) {
	s.RunningSince = &now
	s.PausedAt = nil
	s.FinishedAt = nil
	s.Status = "active"
	s.LastModified = now
}

func (s *ScoreSession) Finish(now time.Time) {
	s.PlayedSeconds = s.DurationSeconds(now)
	s.RunningSince = nil
	s.PausedAt = nil
	s.Status = "finished"
	s.FinishedAt = &now
	s.LastModified = now
}
