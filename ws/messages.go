package ws

import (
	"encoding/json"
	"log/slog"
)

const (
	TypeRoomJoin             = "room.join"
	TypeRoomLeave            = "room.leave"
	TypeBeaconGlobal         = "beacon.global"
	TypeBeaconUser           = "beacon.user"
	TypeQueryInvalidate      = "query.invalidate"
	TypeAdminSpyMessage      = "admin.spy.message"
	TypeAdminSpyDisconnected = "admin.spy.disconnected"
)

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// NewMessage creates a WSMessage with a JSON-encoded payload.
func NewMessage(msgType string, payload any) WSMessage {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("failed to marshal ws payload", "error", err, "type", msgType)
		data = json.RawMessage("null")
	}
	return WSMessage{Type: msgType, Payload: data}
}

func decodePayload(raw json.RawMessage, target any) error {
	return json.Unmarshal(raw, target)
}

// --- Payload types ---

type RoomPayload struct {
	Room string `json:"room"`
}

// QueryInvalidatePayload tells the frontend to refetch a Tanstack Query key.
type QueryInvalidatePayload struct {
	QueryKey []string `json:"queryKey"`
}

// BeaconPayload carries a beacon ping message.
type BeaconPayload struct {
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message"`
}

// SpyMessagePayload carries a spied message for admin observers.
type SpyMessagePayload struct {
	ClientID     string          `json:"clientId"`
	UserID       string          `json:"userId"`
	OriginalType string          `json:"originalType"`
	Payload      json.RawMessage `json:"payload"`
	Timestamp    int64           `json:"timestamp"`
	Direction    string          `json:"direction" tstype:"'inbound' | 'outbound'"`
}

// --- Admin API shapes ---

// ClientInfo holds public metadata about a connected client.
type ClientInfo struct {
	ID          string   `json:"id"`
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	Rooms       []string `json:"rooms"`
	ConnectedAt int64    `json:"connected_at"`
}
