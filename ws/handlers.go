package ws

import (
	"tablescore-api/auth"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

type Handlers struct {
	Hub            *Hub
	JWTSecret      string
	AllowedOrigins []string // "scheme://host" or "host" patterns; empty = accept any origin (dev)
}

// Upgrade authenticates from the access token cookie when one is present and
// otherwise admits the socket anonymously, then hands it to the hub.
func (h *Handlers) Upgrade(c *gin.Context) {
	var userID, email, role string

	if token := auth.AccessTokenFromRequest(c); token != "" {
		if claims, err := auth.ParseAccessToken(token, h.JWTSecret); err == nil {
			userID, email, role = claims.UserID, claims.Email, claims.Role
		}
	}

	opts := &websocket.AcceptOptions{}
	if len(h.AllowedOrigins) == 0 {
		opts.InsecureSkipVerify = true
	} else {
		opts.OriginPatterns = h.AllowedOrigins
	}
	conn, err := websocket.Accept(c.Writer, c.Request, opts)
	if err != nil {
		return
	}

	client := NewClient(h.Hub, conn, userID, email, role)
	ctx := h.Hub.Context()
	select {
	case h.Hub.register <- client:
	case <-ctx.Done():
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}

	go client.WritePump(ctx)
	client.ReadPump(ctx) // Blocks until disconnect
}
