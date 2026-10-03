package domain

import (
	"strings"
	"time"
)

// StatusWaiting is a game created to wait for its players: no clock and no
// scoring until everyone joined or the host starts it.
const StatusWaiting = "waiting"

// NewScoreSession starts the clock right away, or waits for the listed
// players when waitForPlayers is set and there is someone to wait for.
// The first player is the host, who is already at the table.
func NewScoreSession(id, tableCode, ruleID string, players []Player, waitForPlayers bool, now time.Time) ScoreSession {
	values := make(map[string]map[string]int, len(players))
	for _, player := range players {
		values[player.ID] = map[string]int{}
	}
	session := ScoreSession{ID: id, TableCode: tableCode, RuleID: ruleID, Players: players, Values: values, Status: "active", RunningSince: &now, CreatedAt: now, LastModified: now}
	if waitForPlayers && len(players) > 1 {
		session.Status, session.RunningSince = StatusWaiting, nil
		session.Players[0].Joined = true
	}
	return session
}

// Join marks the player with that name as joined, or adds them when the host
// did not list them, and starts a waiting game once everyone is there. It
// reports the player and whether the session changed.
func (s *ScoreSession) Join(name, newID string, now time.Time) (Player, bool) {
	for i, player := range s.Players {
		if !strings.EqualFold(player.Name, name) {
			continue
		}
		if s.Status != StatusWaiting || player.Joined {
			return player, false
		}
		s.Players[i].Joined = true
		s.startWhenEveryoneJoined(now)
		return s.Players[i], true
	}
	player := Player{ID: newID, Name: name, Joined: true}
	s.Players = append(s.Players, player)
	if s.Values == nil {
		s.Values = map[string]map[string]int{}
	}
	s.Values[player.ID] = map[string]int{}
	s.LastModified = now
	s.startWhenEveryoneJoined(now)
	return player, true
}

// Start lets the host begin without waiting for missing players.
func (s *ScoreSession) Start(now time.Time) bool {
	if s.Status != StatusWaiting {
		return false
	}
	s.Resume(now)
	return true
}

func (s *ScoreSession) startWhenEveryoneJoined(now time.Time) {
	if s.Status != StatusWaiting {
		return
	}
	for _, player := range s.Players {
		if !player.Joined {
			s.LastModified = now
			return
		}
	}
	s.Resume(now)
}
