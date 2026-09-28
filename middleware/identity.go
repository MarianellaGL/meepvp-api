package middleware

import (
	"github.com/gin-gonic/gin"
)

// Identity is the authenticated caller as established by RequireAuth. The
// UUID is parsed once here so handlers never repeat the string juggling.
type Identity struct {
	UserID string
	Email  string
	Role   string
}

// Context keys owned by this package.
const (
	ctxIdentity  = "identity"
	ctxRequestID = "request_id"
)

func setIdentity(c *gin.Context, id Identity) { c.Set(ctxIdentity, id) }

// IdentityFrom returns the caller RequireAuth stored. ok is false on routes
// that did not pass through RequireAuth.
func IdentityFrom(c *gin.Context) (Identity, bool) {
	v, exists := c.Get(ctxIdentity)
	if !exists {
		return Identity{}, false
	}
	id, ok := v.(Identity)
	return id, ok
}
