package domain

import (
	"testing"
	"time"
)

func TestWaitingGameStartsWhenTheLastListedPlayerJoins(t *testing.T) {
	now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	session := NewScoreSession("s", "T", "r", []Player{{ID: "host", Name: "Mariana"}, {ID: "p2", Name: "Lucía"}, {ID: "p3", Name: "Tomás"}}, true, now)
	if session.Status != StatusWaiting || session.RunningSince != nil || !session.Players[0].Joined {
		t.Fatalf("game must wait with the host joined: %#v", session)
	}
	if _, changed := session.Join("lucía", "unused", now.Add(time.Minute)); !changed || session.Status != StatusWaiting {
		t.Fatalf("one player missing, still waiting: %#v", session)
	}
	if _, changed := session.Join("LUCÍA", "unused", now); changed {
		t.Fatal("joining twice changes nothing")
	}
	started := now.Add(2 * time.Minute)
	session.Join("Tomás", "unused", started)
	if session.Status != "active" || session.RunningSince == nil || !session.RunningSince.Equal(started) {
		t.Fatalf("game must start when everyone joined: %#v", session)
	}
	if session.DurationSeconds(started.Add(30*time.Second)) != 30 {
		t.Fatal("waiting time must not count as played time")
	}
}

func TestUnlistedPlayersJoinAndHostCanStartEarly(t *testing.T) {
	now := time.Now().UTC()
	session := NewScoreSession("s", "T", "r", []Player{{ID: "host", Name: "Mariana"}, {ID: "p2", Name: "Lucía"}}, true, now)
	player, _ := session.Join("Santiago", "new", now)
	if player.ID != "new" || !player.Joined || len(session.Players) != 3 || session.Status != StatusWaiting {
		t.Fatalf("an unlisted player joins without starting the game: %#v", session)
	}
	if !session.Start(now) || session.Status != "active" {
		t.Fatal("the host can start without waiting")
	}
	if session.Start(now) {
		t.Fatal("an active game cannot be started again")
	}
	solo := NewScoreSession("s", "T", "r", []Player{{ID: "host", Name: "Mariana"}}, true, now)
	if solo.Status != "active" {
		t.Fatal("a game with only the host has nobody to wait for")
	}
}
