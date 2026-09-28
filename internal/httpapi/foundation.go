package httpapi

import (
	"github.com/gin-gonic/gin"
	"tablescore-api/handlers"
)

// WithFoundation mounts the complete shared backend on this API's Gin router.
func (a *API) WithFoundation(d handlers.Deps) (*API, error) {
	if len(d.Cfg.Server.TrustedProxies) > 0 {
		probe := gin.New()
		if err := probe.SetTrustedProxies(d.Cfg.Server.TrustedProxies); err != nil {
			return nil, err
		}
	}
	a.foundation = &d
	return a, nil
}
