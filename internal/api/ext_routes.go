package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterExt serves GET /api/ext: the plugin discovery listing (ids, versions, specs, capability flags; no
// settings values and no private data). It is registered through Deps.Extra, so it sits behind the session guard.
func RegisterExt(e *gin.Engine, listing func() any) {
	e.GET("/api/ext", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"kinds": listing()})
	})
}
