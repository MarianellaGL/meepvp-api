package ws

import (
	"context"
	"time"
)

// Beacons are a demo of the ephemeral-state pattern (Pattern C in the
// WebSocket docs): the server pushes a timestamp to every socket and to each
// connected user's personal room on a fixed interval, and the frontend renders
// it with no REST round trip. Delete this file, the two beacon message types
// and the frontend beacon store if a project does not need them.

// BeaconSettings is read on every tick, so an admin change takes effect within
// one interval. It is an interface so this package never imports services —
// services will import ws to broadcast, and the edge must point that way.
type BeaconSettings interface {
	BeaconsEnabled() bool
	BeaconInterval() time.Duration
}

// RunBeacons drives both beacon loops until ctx is cancelled. Pass the hub's
// context so the loops stop on shutdown.
func RunBeacons(ctx context.Context, hub *Hub, s BeaconSettings) {
	go runBeacon(ctx, s, func() {
		hub.Broadcast("", NewMessage(TypeBeaconGlobal, BeaconPayload{
			Timestamp: time.Now().UnixMilli(),
			Message:   "global ping",
		}), nil)
	})
	runBeacon(ctx, s, func() {
		for _, uid := range hub.ConnectedUserIDs() {
			hub.Broadcast(RoomPrefixUser+uid, NewMessage(TypeBeaconUser, BeaconPayload{
				Timestamp: time.Now().UnixMilli(),
				Message:   "user ping",
			}), nil)
		}
	})
}

func runBeacon(ctx context.Context, s BeaconSettings, tick func()) {
	timer := time.NewTimer(s.BeaconInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if s.BeaconsEnabled() {
				tick()
			}
			timer.Reset(s.BeaconInterval())
		}
	}
}
