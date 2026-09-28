package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		slog.Warn("failed to generate random ID, using fallback", "error", err)
	}
	return hex.EncodeToString(b)
}

const (
	writeTimeout   = 10 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 32768
)

type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan WSMessage
	ID          string
	UserID      string
	Email       string
	Role        string
	ConnectedAt time.Time
}

func NewClient(hub *Hub, conn *websocket.Conn, userID, email, role string) *Client {
	return &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan WSMessage, 256),
		ID:          generateID(),
		UserID:      userID,
		Email:       email,
		Role:        role,
		ConnectedAt: time.Now(),
	}
}

// ReadPump reads messages from the WebSocket and forwards to the Hub.
func (c *Client) ReadPump(ctx context.Context) {
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-ctx.Done():
		}
		_ = c.conn.CloseNow()
	}()

	c.conn.SetReadLimit(maxMessageSize)

	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Warn("invalid ws message", "error", err, "user_id", c.UserID)
			continue
		}

		c.hub.inbound <- ClientMessage{Client: c, Message: msg}
	}
}

// WritePump writes messages from the Hub to the WebSocket.
func (c *Client) WritePump(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.CloseNow()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				slog.Warn("failed to marshal ws message", "error", err, "type", msg.Type)
				continue
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err = c.conn.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}

		case <-ctx.Done():
			return
		}
	}
}
