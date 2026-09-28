package ws

import (
	"context"
	"testing"
	"time"
)

type fakeBeaconSettings struct {
	enabled  bool
	interval time.Duration
}

func (f fakeBeaconSettings) BeaconsEnabled() bool          { return f.enabled }
func (f fakeBeaconSettings) BeaconInterval() time.Duration { return f.interval }

func TestRunBeacons_DeliversGlobalAndUserBeacons(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	c := newTestClientFor(h, 64, "u1")
	h.register <- c
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 1 })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunBeacons(ctx, h, fakeBeaconSettings{enabled: true, interval: 5 * time.Millisecond})

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for !seen[TypeBeaconGlobal] || !seen[TypeBeaconUser] {
		select {
		case msg := <-c.send:
			seen[msg.Type] = true
		case <-deadline:
			t.Fatalf("beacons not received, seen=%v", seen)
		}
	}
}

func TestRunBeacons_DisabledSendsNothing(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	c := newTestClientFor(h, 64, "u1")
	h.register <- c
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 1 })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunBeacons(ctx, h, fakeBeaconSettings{enabled: false, interval: 5 * time.Millisecond})

	select {
	case msg := <-c.send:
		t.Fatalf("unexpected message %s while disabled", msg.Type)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRunBeacons_StopsOnCancel(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunBeacons(ctx, h, fakeBeaconSettings{enabled: true, interval: time.Millisecond})
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunBeacons did not return after cancel")
	}
}
