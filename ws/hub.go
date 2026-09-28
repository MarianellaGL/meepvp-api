package ws

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"tablescore-api/auth"

	"github.com/coder/websocket"
)

type Hub struct {
	clients map[*Client]bool

	// Room memberships: room name -> set of clients
	rooms map[string]map[*Client]bool

	register   chan *Client
	unregister chan *Client

	// User IDs whose sockets must be torn down (role change, delete)
	disconnectUser chan string

	inbound chan ClientMessage

	// For broadcasting from services (outside the hub goroutine)
	broadcast chan BroadcastRequest

	// Context for graceful shutdown — cancels all client goroutines
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.RWMutex
}

type ClientMessage struct {
	Client  *Client
	Message WSMessage
}

type BroadcastRequest struct {
	Room    string // Target room ("" for all clients)
	Message WSMessage
	Exclude *Client // Optional: exclude this client from broadcast
}

func NewHub() *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		clients:        make(map[*Client]bool),
		rooms:          make(map[string]map[*Client]bool),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		disconnectUser: make(chan string, 16),
		inbound:        make(chan ClientMessage, 256),
		broadcast:      make(chan BroadcastRequest, 256),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Context returns the Hub's context, used by client goroutines for graceful shutdown.
func (h *Hub) Context() context.Context {
	return h.ctx
}

// Shutdown cancels the Hub's context: client goroutines, the beacon loops and Run itself all stop.
func (h *Hub) Shutdown() {
	h.cancel()
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.ctx.Done():
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			// Auto-join the user's personal room (skip for anonymous)
			if client.UserID != "" {
				room := RoomPrefixUser + client.UserID
				if h.rooms[room] == nil {
					h.rooms[room] = make(map[*Client]bool)
				}
				h.rooms[room][client] = true
			}
			h.mu.Unlock()
			slog.Info("client connected", "user_id", client.UserID)

		case client := <-h.unregister:
			h.mu.Lock()
			h.removeClient(client)
			h.mu.Unlock()

		case uid := <-h.disconnectUser:
			h.mu.Lock()
			var revoked []*websocket.Conn
			for client := range h.clients {
				if client.UserID != uid {
					continue
				}
				h.removeClient(client)
				// Hub tests build clients without a connection.
				if client.conn != nil {
					revoked = append(revoked, client.conn)
				}
			}
			h.mu.Unlock()
			// Close performs the closing handshake — up to 10s of waiting on a
			// peer that may never answer. Never on the hub goroutine, which
			// every other client's traffic is queued behind.
			for _, conn := range revoked {
				go func(conn *websocket.Conn) {
					_ = conn.Close(websocket.StatusPolicyViolation, "session revoked")
				}(conn)
			}

		case cm := <-h.inbound:
			h.mu.Lock()
			h.handleMessage(cm)
			h.mu.Unlock()

		case br := <-h.broadcast:
			h.mu.Lock()
			h.broadcastToRoom(br)
			h.mu.Unlock()
		}
	}
}

func (h *Hub) handleMessage(cm ClientMessage) {
	// Forward inbound messages to spy observers
	h.forwardToSpy(cm.Client, cm.Message, "inbound")

	switch cm.Message.Type {
	case TypeRoomJoin:
		var payload RoomPayload
		if err := decodePayload(cm.Message.Payload, &payload); err != nil {
			return
		}
		h.joinRoom(cm.Client, payload.Room)

	case TypeRoomLeave:
		var payload RoomPayload
		if err := decodePayload(cm.Message.Payload, &payload); err != nil {
			return
		}
		h.leaveRoom(cm.Client, payload.Room)
	}
}

func (h *Hub) joinRoom(client *Client, room string) {
	// Personal rooms never accept another user's subscription.
	if strings.HasPrefix(room, RoomPrefixUser) && (client.UserID == "" || room != RoomPrefixUser+client.UserID) {
		return
	}
	// Guard admin rooms — require admin role
	if strings.HasPrefix(room, RoomPrefixAdmin) && client.Role != auth.RoleAdmin {
		slog.Warn("non-admin tried to join admin room", "user_id", client.UserID, "room", room)
		return
	}
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[*Client]bool)
	}
	h.rooms[room][client] = true
	slog.Debug("client joined room", "user_id", client.UserID, "room", room)
}

func (h *Hub) leaveRoom(client *Client, room string) {
	if members, ok := h.rooms[room]; ok {
		delete(members, client)
		if len(members) == 0 {
			delete(h.rooms, room)
		}
	}
}

func (h *Hub) broadcastToRoom(br BroadcastRequest) {
	var targets map[*Client]bool

	if br.Room == "" {
		targets = h.clients
	} else {
		targets = h.rooms[br.Room]
	}

	for client := range targets {
		if client == br.Exclude {
			continue
		}
		if _, ok := h.clients[client]; !ok {
			continue
		}
		select {
		case client.send <- br.Message:
		default:
			// Client send buffer full — disconnect
			h.removeClient(client)
			continue
		}
		// Forward outbound messages to spy observers
		h.forwardToSpy(client, br.Message, "outbound")
	}
}

// removeClient fully detaches a client: notifies spy observers, drops it from
// the client set, closes its send channel, and removes it from every room.
// Idempotent. Caller must hold h.mu.
func (h *Hub) removeClient(client *Client) {
	if _, ok := h.clients[client]; !ok {
		return
	}
	spyRoom := RoomPrefixAdminSpy + client.ID
	if observers := h.rooms[spyRoom]; len(observers) > 0 {
		disconnectMsg := NewMessage(TypeAdminSpyDisconnected, map[string]string{"clientId": client.ID})
		for observer := range observers {
			select {
			case observer.send <- disconnectMsg:
			default:
			}
		}
	}
	delete(h.clients, client)
	close(client.send)
	for room, members := range h.rooms {
		delete(members, client)
		if len(members) == 0 {
			delete(h.rooms, room)
		}
	}
	slog.Info("client disconnected", "user_id", client.UserID)
}

// forwardToSpy sends a copy of a message to any admin observers in the spy room.
func (h *Hub) forwardToSpy(client *Client, msg WSMessage, direction string) {
	spyRoom := RoomPrefixAdminSpy + client.ID
	observers := h.rooms[spyRoom]
	if len(observers) == 0 {
		return
	}

	spyMsg := NewMessage(TypeAdminSpyMessage, SpyMessagePayload{
		ClientID:     client.ID,
		UserID:       client.UserID,
		OriginalType: msg.Type,
		Payload:      msg.Payload,
		Timestamp:    time.Now().UnixMilli(),
		Direction:    direction,
	})

	for observer := range observers {
		if _, ok := h.clients[observer]; !ok {
			continue
		}
		select {
		case observer.send <- spyMsg:
		default:
		}
	}
}

// Broadcast is the public API for services to send messages.
// Safe to call from any goroutine.
func (h *Hub) Broadcast(room string, msg WSMessage, exclude *Client) {
	h.broadcast <- BroadcastRequest{Room: room, Message: msg, Exclude: exclude}
}

// DisconnectUser closes every socket belonging to a user. The client's role is
// captured at upgrade time, so a demoted or deleted user would otherwise keep
// an admin-grade socket until they disconnect on their own. Safe to call from
// any goroutine; non-admin sockets simply reconnect with the new role.
func (h *Hub) DisconnectUser(userID string) {
	h.disconnectUser <- userID
}

// ConnectedClients returns metadata for all connected clients.
func (h *Hub) ConnectedClients() []ClientInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Build reverse index: client -> rooms
	clientRooms := make(map[*Client][]string)
	for room, members := range h.rooms {
		for client := range members {
			clientRooms[client] = append(clientRooms[client], room)
		}
	}

	infos := make([]ClientInfo, 0, len(h.clients))
	for client := range h.clients {
		rooms := clientRooms[client]
		if rooms == nil {
			rooms = []string{} // JSON [] not null; the admin UI reads rooms.length
		}
		infos = append(infos, ClientInfo{
			ID:          client.ID,
			UserID:      client.UserID,
			Email:       client.Email,
			Rooms:       rooms,
			ConnectedAt: client.ConnectedAt.UnixMilli(),
		})
	}
	return infos
}

// ConnectedUserIDs returns the unique user IDs of all connected clients.
func (h *Hub) ConnectedUserIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[string]bool)
	for client := range h.clients {
		if client.UserID != "" {
			seen[client.UserID] = true
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}
