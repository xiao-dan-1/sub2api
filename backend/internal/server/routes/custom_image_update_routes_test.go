//go:build unit

package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterSystemRoutesIncludesCustomImageEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		System:            &adminhandler.SystemHandler{},
		CustomImageUpdate: &adminhandler.CustomImageUpdateHandler{},
	}}

	registerSystemRoutes(router.Group("/api/v1/admin"), handlers)

	routes := router.Routes()
	routeHandlers := make(map[string]string, len(routes))
	for _, route := range routes {
		routeHandlers[route.Method+" "+route.Path] = route.Handler
	}
	require.Equal(
		t,
		"github.com/Wei-Shaw/sub2api/internal/handler/admin.(*CustomImageUpdateHandler).Check-fm",
		routeHandlers["GET /api/v1/admin/system/custom-image/check"],
	)
	require.Equal(
		t,
		"github.com/Wei-Shaw/sub2api/internal/handler/admin.(*CustomImageUpdateHandler).PerformUpdate-fm",
		routeHandlers["POST /api/v1/admin/system/custom-image/update"],
	)
}
