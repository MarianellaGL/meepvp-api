package ws

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// waitFor polls until cond is true or the timeout elapses.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func newTestClient(h *Hub, buf int) *Client {
	return newTestClientFor(h, buf, "u1")
}

// newTestClientFor builds a connection-less client for a given user id.
func newTestClientFor(h *Hub, buf int, userID string) *Client {
	return &Client{hub: h, send: make(chan WSMessage, buf), ID: generateID(), UserID: userID, ConnectedAt: time.Now()}
}

func (h *Hub) roomHas(room string, c *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rooms[room][c]
}

func TestBroadcast_FullBufferRemovesClientFromRooms(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	c := newTestClient(h, 1) // buffer of 1 so the second message overflows
	h.register <- c
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 1 })

	h.inbound <- ClientMessage{Client: c, Message: NewMessage(TypeRoomJoin, RoomPayload{Room: "r1"})}
	waitFor(t, func() bool { return h.roomHas("r1", c) })

	msg := NewMessage(TypeBeaconGlobal, BeaconPayload{Timestamp: 1})
	h.Broadcast("r1", msg, nil) // fills the buffer
	h.Broadcast("r1", msg, nil) // overflows → client must be dropped everywhere
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 0 && !h.roomHas("r1", c) })

	// Regression: this used to panic the hub goroutine (send on closed channel).
	h.Broadcast("r1", msg, nil)
	h.Broadcast("", msg, nil)
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 0 })

	// Unregister after removal must be a no-op, not a double close.
	h.unregister <- c
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 0 })
}

func TestUnregister_RemovesFromAllRooms(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	c := newTestClient(h, 8)
	h.register <- c
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 1 })
	h.inbound <- ClientMessage{Client: c, Message: NewMessage(TypeRoomJoin, RoomPayload{Room: "a"})}
	h.inbound <- ClientMessage{Client: c, Message: NewMessage(TypeRoomJoin, RoomPayload{Room: "b"})}
	waitFor(t, func() bool { return h.roomHas("a", c) && h.roomHas("b", c) })

	h.unregister <- c
	waitFor(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients) == 0 && len(h.rooms) == 0 // the auto-joined "user:u1" room must be gone too
	})
}

func TestDisconnectUser_RemovesAllClientsForUser(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	a := newTestClientFor(h, 8, "u1")
	b := newTestClientFor(h, 8, "u1")
	other := newTestClientFor(h, 8, "u2")
	h.register <- a
	h.register <- b
	h.register <- other
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 3 })

	h.DisconnectUser("u1")
	waitFor(t, func() bool {
		ids := h.ConnectedUserIDs()
		return len(ids) == 1 && ids[0] == "u2"
	})

	// Both of u1's clients are gone, and u1's personal room went with them.
	waitFor(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients) == 1 && h.rooms[RoomPrefixUser+"u1"] == nil
	})
}

func TestRegister_DoesNotBlockAfterShutdown(t *testing.T) {
	h := NewHub()
	h.Shutdown() // Run() was never started, so nothing ever drains h.register

	done := make(chan struct{})
	go func() {
		defer close(done)
		c := newTestClient(h, 1)
		select {
		case h.register <- c:
		case <-h.Context().Done():
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("register send blocked after Shutdown")
	}
}

func TestUnregister_DoesNotBlockAfterShutdown(t *testing.T) {
	h := NewHub()
	h.Shutdown() // Run() was never started, so nothing ever drains h.unregister

	done := make(chan struct{})
	go func() {
		defer close(done)
		c := newTestClient(h, 1)
		select {
		case h.unregister <- c:
		case <-h.Context().Done():
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unregister send blocked after Shutdown")
	}
}

func TestRun_ReturnsOnShutdown(t *testing.T) {
	h := NewHub()
	done := make(chan struct{})
	go func() {
		h.Run()
		close(done)
	}()
	h.Shutdown()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Shutdown")
	}
}

// Regression: a client in no rooms must serialize rooms as [] not null — the admin
// connections table reads rooms.length and crashed the page.
func TestConnectedClients_RoomsNeverNil(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Shutdown()

	h.register <- newTestClientFor(h, 1, "")
	waitFor(t, func() bool { return len(h.ConnectedClients()) == 1 })

	b, err := json.Marshal(h.ConnectedClients()[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"rooms":[]`) {
		t.Fatalf("rooms not an empty array: %s", b)
	}
}

func TestPersonalRoomsRejectOtherUsers(t *testing.T) {
	h := NewHub()
	c := newTestClientFor(h, 8, "u1")
	h.joinRoom(c, RoomPrefixUser+"u2")
	if h.roomHas(RoomPrefixUser+"u2", c) {
		t.Fatal("another user's personal room was joined")
	}
	h.joinRoom(c, RoomPrefixUser+"u1")
	if !h.roomHas(RoomPrefixUser+"u1", c) {
		t.Fatal("own personal room should be available")
	}
}
