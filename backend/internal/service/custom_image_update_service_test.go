//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type customImageTagClientStub struct {
	tags            []string
	listErr         error
	images          []string
	references      []string
	manifestDigests map[string]string
	manifestErrs    map[string]error
}

func (s *customImageTagClientStub) ListTags(_ context.Context, image string) ([]string, error) {
	s.images = append(s.images, image)
	return append([]string(nil), s.tags...), s.listErr
}

func (s *customImageTagClientStub) ManifestDigest(_ context.Context, image, reference string) (string, error) {
	s.images = append(s.images, image)
	s.references = append(s.references, reference)
	if err := s.manifestErrs[reference]; err != nil {
		return "", err
	}
	return s.manifestDigests[reference], nil
}

type customImageUpdaterStub struct {
	configured bool
	err        error
	triggered  chan struct{}
	block      chan struct{}
	contextErr error
	deadline   time.Time
}

func (s *customImageUpdaterStub) Configured() bool {
	return s.configured
}

func (s *customImageUpdaterStub) TriggerUpdate(ctx context.Context) error {
	s.contextErr = ctx.Err()
	s.deadline, _ = ctx.Deadline()
	if s.triggered != nil {
		s.triggered <- struct{}{}
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
}

func newCustomImageUpdateServiceForTest(
	currentVersion string,
	tagClient ContainerTagClient,
	updater ContainerUpdater,
) *CustomImageUpdateService {
	return NewCustomImageUpdateService(currentVersion, CustomImageUpdateServiceOptions{
		CustomRepo:  "xiao-dan-1/sub2api",
		CustomImage: "ghcr.io/xiao-dan-1/sub2api",
		TagClient:   tagClient,
		Updater:     updater,
	})
}

func TestCustomImageUpdateServiceCheckSelectsHighestValidPublishedPackage(t *testing.T) {
	tagClient := &customImageTagClientStub{
		tags: []string{
			"latest",
			"0.1.161-xd.99",
			"0.1.162-xd.2",
			"0.1.162-xd.10-amd64",
			"0.1.162-xd.invalid",
			"v0.1.162-xd.10",
		},
		manifestDigests: map[string]string{
			"v0.1.162-xd.10": "sha256:ready",
			"latest":         "sha256:ready",
		},
	}
	svc := newCustomImageUpdateServiceForTest(
		"0.1.161-xd.4",
		tagClient,
		&customImageUpdaterStub{configured: true},
	)

	info, err := svc.Check(context.Background())

	require.NoError(t, err)
	require.Equal(t, "0.1.161-xd.4", info.CurrentVersion)
	require.Equal(t, "0.1.162-xd.10", info.TargetVersion)
	require.Equal(t, "ghcr.io/xiao-dan-1/sub2api", info.Image)
	require.Equal(t, "ghcr.io/xiao-dan-1/sub2api:v0.1.162-xd.10", info.TargetImage)
	require.Equal(t, "sha256:ready", info.TargetDigest)
	require.Equal(t, "https://github.com/xiao-dan-1/sub2api/releases/tag/v0.1.162-xd.10", info.ReleaseURL)
	require.True(t, info.HasUpdate)
	require.True(t, info.TargetReady)
	require.True(t, info.LatestAliasReady)
	require.True(t, info.Ready)
	require.Empty(t, info.Warning)
	require.Equal(t, []string{
		"ghcr.io/xiao-dan-1/sub2api",
		"ghcr.io/xiao-dan-1/sub2api",
		"ghcr.io/xiao-dan-1/sub2api",
	}, tagClient.images)
	require.Equal(t, []string{"v0.1.162-xd.10", "latest"}, tagClient.references)
}

func TestCustomImageUpdateServiceCheckReportsNoUpdateAtNewestRevision(t *testing.T) {
	tagClient := &customImageTagClientStub{
		tags: []string{"0.1.162-xd.9", "v0.1.162-xd.10"},
	}
	svc := newCustomImageUpdateServiceForTest(
		"0.1.162-xd.10",
		tagClient,
		&customImageUpdaterStub{configured: true},
	)

	info, err := svc.Check(context.Background())

	require.NoError(t, err)
	require.Equal(t, "0.1.162-xd.10", info.TargetVersion)
	require.False(t, info.HasUpdate)
	require.False(t, info.Ready)
	require.Empty(t, tagClient.references, "manifest readiness is unnecessary when no update exists")
}

func TestCustomImageUpdateServiceCheckWaitsUntilLatestAliasMatchesExactTag(t *testing.T) {
	tagClient := &customImageTagClientStub{
		tags: []string{"0.1.162-xd.5"},
		manifestDigests: map[string]string{
			"0.1.162-xd.5": "sha256:exact",
			"latest":       "sha256:older",
		},
	}
	svc := newCustomImageUpdateServiceForTest(
		"0.1.162-xd.4",
		tagClient,
		&customImageUpdaterStub{configured: true},
	)

	info, err := svc.Check(context.Background())

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.True(t, info.TargetReady)
	require.False(t, info.LatestAliasReady)
	require.False(t, info.Ready)
	require.Contains(t, info.Warning, "latest")
}

func TestCustomImageUpdateServiceCheckReportsIncompleteConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		service *CustomImageUpdateService
		warning string
	}{
		{
			name: "image",
			service: NewCustomImageUpdateService("0.1.162-xd.4", CustomImageUpdateServiceOptions{
				TagClient: &customImageTagClientStub{},
				Updater:   &customImageUpdaterStub{configured: true},
			}),
			warning: "image",
		},
		{
			name: "tag client",
			service: NewCustomImageUpdateService("0.1.162-xd.4", CustomImageUpdateServiceOptions{
				CustomImage: "ghcr.io/xiao-dan-1/sub2api",
				Updater:     &customImageUpdaterStub{configured: true},
			}),
			warning: "discovery",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := tt.service.Check(context.Background())

			require.NoError(t, err)
			require.False(t, info.Ready)
			require.Contains(t, info.Warning, tt.warning)
		})
	}
}

func TestCustomImageUpdateServiceTriggerRechecksReadinessAndWaitsForWatchtower(t *testing.T) {
	tagClient := &customImageTagClientStub{
		tags: []string{"0.1.162-xd.5"},
		manifestDigests: map[string]string{
			"0.1.162-xd.5": "sha256:ready",
			"latest":       "sha256:ready",
		},
	}
	triggered := make(chan struct{}, 1)
	block := make(chan struct{})
	updater := &customImageUpdaterStub{
		configured: true,
		triggered:  triggered,
		block:      block,
	}
	svc := newCustomImageUpdateServiceForTest("0.1.162-xd.4", tagClient, updater)

	first, err := svc.Check(context.Background())
	require.NoError(t, err)
	require.Equal(t, "0.1.162-xd.5", first.TargetVersion)

	tagClient.tags = []string{"0.1.162-xd.6"}
	tagClient.manifestDigests = map[string]string{
		"0.1.162-xd.6": "sha256:new",
		"latest":       "sha256:new",
	}

	type triggerCall struct {
		result *CustomImageUpdateResult
		err    error
	}
	resultCh := make(chan triggerCall, 1)
	go func() {
		result, triggerErr := svc.Trigger(context.Background())
		resultCh <- triggerCall{result: result, err: triggerErr}
	}()

	select {
	case <-triggered:
	case call := <-resultCh:
		t.Fatalf("Trigger returned before Watchtower was called: result=%+v err=%v", call.result, call.err)
	case <-time.After(time.Second):
		t.Fatal("Watchtower update was not triggered")
	}
	select {
	case call := <-resultCh:
		t.Fatalf("Trigger returned before Watchtower completed: result=%+v err=%v", call.result, call.err)
	default:
	}

	close(block)
	call := <-resultCh
	require.NoError(t, call.err)
	require.Equal(t, &CustomImageUpdateResult{
		TargetVersion:    "0.1.162-xd.6",
		TargetImage:      "ghcr.io/xiao-dan-1/sub2api:0.1.162-xd.6",
		TargetDigest:     "sha256:new",
		AutomaticRestart: true,
	}, call.result)
}

func TestCustomImageUpdateServiceTriggerDetachesFromCanceledRequestWithTenMinuteBound(t *testing.T) {
	tagClient := &customImageTagClientStub{
		tags: []string{"0.1.162-xd.5"},
		manifestDigests: map[string]string{
			"0.1.162-xd.5": "sha256:ready",
			"latest":       "sha256:ready",
		},
	}
	updater := &customImageUpdaterStub{configured: true}
	svc := newCustomImageUpdateServiceForTest("0.1.162-xd.4", tagClient, updater)
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()

	result, err := svc.Trigger(requestCtx)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, updater.contextErr)
	require.WithinDuration(t, started.Add(10*time.Minute), updater.deadline, 2*time.Second)
}

func TestCustomImageUpdateServiceTriggerRejectsUnreadyOrUnconfiguredUpdates(t *testing.T) {
	t.Run("latest alias not ready", func(t *testing.T) {
		svc := newCustomImageUpdateServiceForTest(
			"0.1.162-xd.4",
			&customImageTagClientStub{
				tags: []string{"0.1.162-xd.5"},
				manifestDigests: map[string]string{
					"0.1.162-xd.5": "sha256:exact",
					"latest":       "sha256:older",
				},
			},
			&customImageUpdaterStub{configured: true},
		)

		result, err := svc.Trigger(context.Background())

		require.Nil(t, result)
		require.ErrorIs(t, err, ErrCustomUpdateNotReady)
	})

	t.Run("updater not configured", func(t *testing.T) {
		svc := newCustomImageUpdateServiceForTest(
			"0.1.162-xd.4",
			&customImageTagClientStub{
				tags: []string{"0.1.162-xd.5"},
				manifestDigests: map[string]string{
					"0.1.162-xd.5": "sha256:ready",
					"latest":       "sha256:ready",
				},
			},
			&customImageUpdaterStub{configured: false},
		)

		result, err := svc.Trigger(context.Background())

		require.Nil(t, result)
		require.ErrorIs(t, err, ErrCustomUpdaterUnavailable)
	})
}

func TestCustomImageUpdateServiceTriggerPropagatesWatchtowerFailure(t *testing.T) {
	triggerErr := errors.New("Watchtower returned 401")
	svc := newCustomImageUpdateServiceForTest(
		"0.1.162-xd.4",
		&customImageTagClientStub{
			tags: []string{"0.1.162-xd.5"},
			manifestDigests: map[string]string{
				"0.1.162-xd.5": "sha256:ready",
				"latest":       "sha256:ready",
			},
		},
		&customImageUpdaterStub{configured: true, err: triggerErr},
	)

	result, err := svc.Trigger(context.Background())

	require.Nil(t, result)
	require.ErrorIs(t, err, triggerErr)
}
