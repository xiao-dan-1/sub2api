//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type customImageUpdateServiceStub struct {
	checkInfo      *service.CustomImageUpdateInfo
	checkErr       error
	checkCalls     int
	triggerResult  *service.CustomImageUpdateResult
	triggerErr     error
	triggerCalls   int
	triggerContext context.Context
}

func (s *customImageUpdateServiceStub) Check(context.Context) (*service.CustomImageUpdateInfo, error) {
	s.checkCalls++
	return s.checkInfo, s.checkErr
}

func (s *customImageUpdateServiceStub) Trigger(ctx context.Context) (*service.CustomImageUpdateResult, error) {
	s.triggerCalls++
	s.triggerContext = ctx
	return s.triggerResult, s.triggerErr
}

type customImageUpdateResponseEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Message          string `json:"message"`
		OperationID      string `json:"operation_id"`
		TargetVersion    string `json:"target_version"`
		TargetImage      string `json:"target_image"`
		TargetDigest     string `json:"target_digest"`
		AutomaticRestart bool   `json:"automatic_restart"`
	} `json:"data"`
}

func newCustomImageUpdateHandlerTestRouter(
	t *testing.T,
	updateSvc *customImageUpdateServiceStub,
	repo *memoryIdempotencyRepoStub,
) (*gin.Engine, *service.SystemOperationLockService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	lockSvc := service.NewSystemOperationLockService(repo, service.IdempotencyConfig{
		ProcessingTimeout:  time.Second,
		SystemOperationTTL: time.Minute,
	})
	handler := NewCustomImageUpdateHandler(updateSvc, lockSvc)
	router := gin.New()
	router.GET("/api/v1/admin/system/custom-image/check", handler.Check)
	router.POST("/api/v1/admin/system/custom-image/update", handler.PerformUpdate)
	return router, lockSvc
}

func TestCustomImageUpdateHandlerCheckReturnsReadinessMetadata(t *testing.T) {
	updateSvc := &customImageUpdateServiceStub{
		checkInfo: &service.CustomImageUpdateInfo{
			CurrentVersion:   "0.1.162-xd.4",
			TargetVersion:    "0.1.162-xd.5",
			Image:            "ghcr.io/xiao-dan-1/sub2api",
			TargetImage:      "ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5",
			TargetDigest:     "sha256:ready",
			ReleaseURL:       "https://github.com/xiao-dan-1/sub2api/releases/tag/v0.1.162-xd.5",
			HasUpdate:        true,
			TargetReady:      true,
			LatestAliasReady: true,
			Ready:            true,
		},
	}
	router, _ := newCustomImageUpdateHandlerTestRouter(t, updateSvc, newMemoryIdempotencyRepoStub())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/custom-image/check", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, updateSvc.checkCalls)
	var body struct {
		Code int                           `json:"code"`
		Data service.CustomImageUpdateInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "0.1.162-xd.5", body.Data.TargetVersion)
	require.Equal(t, "sha256:ready", body.Data.TargetDigest)
	require.True(t, body.Data.Ready)
	require.NotContains(t, rec.Body.String(), "watchtower")
	require.NotContains(t, rec.Body.String(), "token")
}

func TestCustomImageUpdateHandlerPerformUpdateReturnsAutomaticRestartMetadata(t *testing.T) {
	updateSvc := &customImageUpdateServiceStub{
		triggerResult: &service.CustomImageUpdateResult{
			TargetVersion:    "0.1.162-xd.5",
			TargetImage:      "ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5",
			TargetDigest:     "sha256:ready",
			AutomaticRestart: true,
		},
	}
	repo := newMemoryIdempotencyRepoStub()
	router, _ := newCustomImageUpdateHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/system/custom-image/update",
		strings.NewReader(`{"image":"evil.example/other:latest","target_version":"9.9.9"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "custom-image-update")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, updateSvc.triggerCalls)
	requireSystemLockStatus(t, repo, service.IdempotencyStatusSucceeded)

	var body customImageUpdateResponseEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "Custom container update scheduled. The service will restart automatically.", body.Data.Message)
	require.NotEmpty(t, body.Data.OperationID)
	require.Equal(t, "0.1.162-xd.5", body.Data.TargetVersion)
	require.Equal(t, "ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5", body.Data.TargetImage)
	require.Equal(t, "sha256:ready", body.Data.TargetDigest)
	require.True(t, body.Data.AutomaticRestart)
	require.NotContains(t, rec.Body.String(), "evil.example")
}

func TestCustomImageUpdateHandlerPerformUpdateDetachesFromRequestCancellation(t *testing.T) {
	updateSvc := &customImageUpdateServiceStub{
		triggerResult: &service.CustomImageUpdateResult{
			TargetVersion:    "0.1.162-xd.5",
			TargetImage:      "ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.5",
			TargetDigest:     "sha256:ready",
			AutomaticRestart: true,
		},
	}
	router, _ := newCustomImageUpdateHandlerTestRouter(t, updateSvc, newMemoryIdempotencyRepoStub())
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/custom-image/update", nil).WithContext(requestCtx)
	req.Header.Set("Idempotency-Key", "canceled-browser")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, updateSvc.triggerCalls)
	require.NotNil(t, updateSvc.triggerContext)
	require.NoError(t, updateSvc.triggerContext.Err())
}

func TestCustomImageUpdateHandlerPerformUpdateRejectsConcurrentSystemOperation(t *testing.T) {
	updateSvc := &customImageUpdateServiceStub{}
	repo := newMemoryIdempotencyRepoStub()
	router, lockSvc := newCustomImageUpdateHandlerTestRouter(t, updateSvc, repo)
	lock, err := lockSvc.Acquire(context.Background(), "existing-system-operation")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = lockSvc.Release(context.Background(), lock, false, "TEST_CLEANUP")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/custom-image/update", nil)
	req.Header.Set("Idempotency-Key", "blocked-custom-update")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Equal(t, 0, updateSvc.triggerCalls)
	require.Contains(t, rec.Body.String(), "SYSTEM_OPERATION_BUSY")
}

func TestCustomImageUpdateHandlerPerformUpdateMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "not ready",
			err:        service.ErrCustomUpdateNotReady,
			wantStatus: http.StatusConflict,
			wantBody:   "CUSTOM_UPDATE_NOT_READY",
		},
		{
			name:       "watchtower failure",
			err:        errors.New("Watchtower request timed out"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateSvc := &customImageUpdateServiceStub{triggerErr: tt.err}
			router, _ := newCustomImageUpdateHandlerTestRouter(t, updateSvc, newMemoryIdempotencyRepoStub())

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/custom-image/update", nil)
			req.Header.Set("Idempotency-Key", "error-"+tt.name)
			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}
