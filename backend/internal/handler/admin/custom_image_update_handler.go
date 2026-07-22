package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type customImageUpdateService interface {
	Check(ctx context.Context) (*service.CustomImageUpdateInfo, error)
	Trigger(ctx context.Context) (*service.CustomImageUpdateResult, error)
}

// CustomImageUpdateHandler exposes the fork-owned image updater separately from upstream binary updates.
type CustomImageUpdateHandler struct {
	updateSvc customImageUpdateService
	lockSvc   *service.SystemOperationLockService
}

// NewCustomImageUpdateHandler creates the custom image admin handler.
func NewCustomImageUpdateHandler(
	updateSvc customImageUpdateService,
	lockSvc *service.SystemOperationLockService,
) *CustomImageUpdateHandler {
	return &CustomImageUpdateHandler{
		updateSvc: updateSvc,
		lockSvc:   lockSvc,
	}
}

// Check returns the verified custom image release state.
// GET /api/v1/admin/system/custom-image/check
func (h *CustomImageUpdateHandler) Check(c *gin.Context) {
	info, err := h.updateSvc.Check(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if info == nil {
		response.Error(c, http.StatusInternalServerError, "custom image update check returned no result")
		return
	}
	response.Success(c, info)
}

// PerformUpdate validates the target again and waits for Watchtower to accept the replacement request.
// POST /api/v1/admin/system/custom-image/update
func (h *CustomImageUpdateHandler) PerformUpdate(c *gin.Context) {
	operationID := buildSystemOperationID(c, "custom-image-update")
	payload := gin.H{"operation_id": operationID}
	executeAdminIdempotentJSON(
		c,
		"admin.system.custom_image.update",
		payload,
		service.DefaultSystemOperationIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			lock, release, err := h.acquireSystemLock(ctx, operationID)
			if err != nil {
				return nil, err
			}
			releaseReason := "CUSTOM_IMAGE_UPDATE_FAILED"
			succeeded := false
			defer func() {
				release(releaseReason, succeeded)
			}()

			result, err := h.updateSvc.Trigger(context.WithoutCancel(ctx))
			if err != nil {
				return nil, err
			}
			if result == nil {
				return nil, fmt.Errorf("custom image update returned no result")
			}
			succeeded = true
			releaseReason = ""

			return gin.H{
				"message":           "Custom container update scheduled. The service will restart automatically.",
				"operation_id":      lock.OperationID(),
				"target_version":    result.TargetVersion,
				"target_image":      result.TargetImage,
				"target_digest":     result.TargetDigest,
				"automatic_restart": result.AutomaticRestart,
			}, nil
		},
	)
}

func (h *CustomImageUpdateHandler) acquireSystemLock(
	ctx context.Context,
	operationID string,
) (*service.SystemOperationLock, func(string, bool), error) {
	if h.lockSvc == nil {
		return nil, nil, service.ErrIdempotencyStoreUnavail
	}
	lock, err := h.lockSvc.Acquire(ctx, operationID)
	if err != nil {
		return nil, nil, err
	}
	release := func(reason string, succeeded bool) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.lockSvc.Release(releaseCtx, lock, succeeded, reason)
	}
	return lock, release, nil
}
