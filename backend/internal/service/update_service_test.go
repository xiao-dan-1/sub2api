//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

type updateServiceTagClientStub struct {
	tags            []string
	err             error
	images          []string
	manifestDigests map[string]string
	manifestErrs    map[string]error
	references      []string
}

func (s *updateServiceTagClientStub) ListTags(_ context.Context, image string) ([]string, error) {
	s.images = append(s.images, image)
	return s.tags, s.err
}

func (s *updateServiceTagClientStub) ManifestDigest(_ context.Context, image, reference string) (string, error) {
	s.images = append(s.images, image)
	s.references = append(s.references, reference)
	if err := s.manifestErrs[reference]; err != nil {
		return "", err
	}
	return s.manifestDigests[reference], nil
}

type updateServiceContainerUpdaterStub struct {
	configured bool
	err        error
	triggered  chan struct{}
	block      chan struct{}
}

func (s *updateServiceContainerUpdaterStub) Configured() bool {
	return s.configured
}

func (s *updateServiceContainerUpdaterStub) TriggerUpdate(context.Context) error {
	if s.triggered != nil {
		s.triggered <- struct{}{}
	}
	if s.block != nil {
		<-s.block
	}
	return s.err
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	_, err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServiceCheckUpdateCustomRevisionMatchesSameBaseVersion(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.153",
				Name:    "v0.1.153",
			},
		},
		"0.1.153-xd.1",
		"release",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Equal(t, "0.1.153-xd.1", info.CurrentVersion)
	require.Equal(t, "0.1.153", info.LatestVersion)
}

func TestUpdateServiceCustomRevisionUsesCustomBuildTypeWhenUpstreamIsNewer(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.154",
				Name:    "v0.1.154",
			},
		},
		"0.1.153-xd.1",
		"release",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, "custom", info.BuildType)
	require.Contains(t, info.Warning, "custom build")
}

func TestUpdateServiceCustomRevisionSelectsHighestExactMatchingPackage(t *testing.T) {
	tagClient := &updateServiceTagClientStub{tags: []string{
		"latest",
		"0.1.156-xd.9",
		"0.1.156-xd.10-amd64",
		"0.1.155-xd.99",
		"0.1.156-xd.invalid",
		"v0.1.156-xd.10",
	}, manifestDigests: map[string]string{
		"v0.1.156-xd.10": "sha256:ready",
		"latest":         "sha256:ready",
	}}
	updater := &updateServiceContainerUpdaterStub{configured: true}
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.156",
				Name:    "v0.1.156",
			},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient:   tagClient,
			Updater:     updater,
		},
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, []string{
		"ghcr.io/xiao-dan-1/sub2api",
		"ghcr.io/xiao-dan-1/sub2api",
		"ghcr.io/xiao-dan-1/sub2api",
	}, tagClient.images)
	require.Equal(t, []string{"v0.1.156-xd.10", "latest"}, tagClient.references)
	require.Equal(t, "0.1.156-xd.10", info.CustomVersion)
	require.Equal(t, "ghcr.io/xiao-dan-1/sub2api", info.CustomImage)
	require.Equal(t, "https://github.com/xiao-dan-1/sub2api/releases/tag/v0.1.156-xd.10", info.CustomReleaseURL)
	require.True(t, info.CustomUpdateAvailable)
	require.True(t, info.CustomUpdateReady)
	require.True(t, info.HasUpdate)
}

func TestUpdateServiceCustomRevisionWaitsUntilLatestAliasMatchesExactTag(t *testing.T) {
	tagClient := &updateServiceTagClientStub{
		tags: []string{"latest", "0.1.156-xd.5"},
		manifestDigests: map[string]string{
			"0.1.156-xd.5": "sha256:exact",
			"latest":       "sha256:older",
		},
	}
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.156", Name: "v0.1.156"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient:   tagClient,
			Updater:     &updateServiceContainerUpdaterStub{configured: true},
		},
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.156-xd.5", info.CustomVersion)
	require.True(t, info.CustomUpdateAvailable)
	require.False(t, info.CustomUpdateReady)
	require.Contains(t, info.CustomUpdateWarning, "latest")
}

func TestUpdateServiceCachedCustomRevisionPreservesLatestAliasNotReadyState(t *testing.T) {
	cache := &updateServiceCacheStub{}
	tagClient := &updateServiceTagClientStub{
		tags: []string{"latest", "0.1.156-xd.5"},
		manifestDigests: map[string]string{
			"0.1.156-xd.5": "sha256:exact",
			"latest":       "sha256:older",
		},
	}
	svc := NewUpdateServiceWithOptions(
		cache,
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.156", Name: "v0.1.156"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient:   tagClient,
			Updater:     &updateServiceContainerUpdaterStub{configured: true},
		},
	)

	_, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	info, err := svc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.True(t, info.Cached)
	require.True(t, info.CustomUpdateAvailable)
	require.False(t, info.CustomUpdateReady)
	require.Contains(t, info.CustomUpdateWarning, "latest")
}

func TestUpdateServiceCustomRevisionWaitsForMatchingUpstreamPackage(t *testing.T) {
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.157", Name: "v0.1.157"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient: &updateServiceTagClientStub{tags: []string{
				"0.1.156-xd.5",
			}},
			Updater: &updateServiceContainerUpdaterStub{configured: true},
		},
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.True(t, info.HasUpdate, "the author's newer release must remain visible")
	require.Empty(t, info.CustomVersion)
	require.False(t, info.CustomUpdateAvailable)
	require.False(t, info.CustomUpdateReady)
	require.Contains(t, info.CustomUpdateWarning, "0.1.157")
}

func TestUpdateServiceCustomRevisionUsesMatchingNewUpstreamPackage(t *testing.T) {
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.157", Name: "v0.1.157"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient: &updateServiceTagClientStub{
				tags: []string{
					"0.1.157-xd.1",
					"0.1.157-xd.2",
				},
				manifestDigests: map[string]string{
					"0.1.157-xd.2": "sha256:ready",
					"latest":       "sha256:ready",
				},
			},
			Updater: &updateServiceContainerUpdaterStub{configured: true},
		},
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.157-xd.2", info.CustomVersion)
	require.True(t, info.CustomUpdateAvailable)
	require.True(t, info.CustomUpdateReady)
}

func TestUpdateServicePerformUpdateWaitsForCustomContainerTrigger(t *testing.T) {
	triggered := make(chan struct{}, 1)
	block := make(chan struct{})
	updater := &updateServiceContainerUpdaterStub{
		configured: true,
		triggered:  triggered,
		block:      block,
	}
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.156", Name: "v0.1.156"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient: &updateServiceTagClientStub{
				tags: []string{
					"0.1.156-xd.5",
				},
				manifestDigests: map[string]string{
					"0.1.156-xd.5": "sha256:ready",
					"latest":       "sha256:ready",
				},
			},
			Updater: updater,
		},
	)

	type updateCallResult struct {
		result *UpdateExecutionResult
		err    error
	}
	resultCh := make(chan updateCallResult, 1)
	go func() {
		result, err := svc.PerformUpdate(context.Background())
		resultCh <- updateCallResult{result: result, err: err}
	}()

	select {
	case <-triggered:
	case call := <-resultCh:
		t.Fatalf("PerformUpdate returned before Watchtower was triggered: result=%+v err=%v", call.result, call.err)
	case <-time.After(time.Second):
		t.Fatal("Watchtower update was not triggered")
	}
	select {
	case call := <-resultCh:
		t.Fatalf("PerformUpdate returned before Watchtower completed: result=%+v err=%v", call.result, call.err)
	default:
	}

	close(block)
	call := <-resultCh
	require.NoError(t, call.err)
	require.Equal(t, &UpdateExecutionResult{
		NeedRestart:      false,
		AutomaticRestart: true,
		TargetVersion:    "0.1.156-xd.5",
		TargetImage:      "ghcr.io/xiao-dan-1/sub2api:latest",
	}, call.result)
}

func TestUpdateServicePerformUpdateReturnsCustomContainerTriggerFailure(t *testing.T) {
	triggerErr := errors.New("Watchtower returned 401")
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.156", Name: "v0.1.156"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient: &updateServiceTagClientStub{
				tags: []string{"0.1.156-xd.5"},
				manifestDigests: map[string]string{
					"0.1.156-xd.5": "sha256:ready",
					"latest":       "sha256:ready",
				},
			},
			Updater: &updateServiceContainerUpdaterStub{
				configured: true,
				err:        triggerErr,
			},
		},
	)

	result, err := svc.PerformUpdate(context.Background())

	require.Nil(t, result)
	require.ErrorIs(t, err, triggerErr)
}

func TestUpdateServicePerformUpdateRejectsCustomBuildUntilPackageIsReady(t *testing.T) {
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.157", Name: "v0.1.157"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient: &updateServiceTagClientStub{
				tags: []string{"0.1.156-xd.5"},
				manifestDigests: map[string]string{
					"0.1.156-xd.5": "sha256:ready",
					"latest":       "sha256:ready",
				},
			},
			Updater: &updateServiceContainerUpdaterStub{configured: true},
		},
	)

	_, err := svc.PerformUpdate(context.Background())

	require.ErrorIs(t, err, ErrCustomUpdateNotReady)
}

func TestUpdateServicePerformUpdateRejectsCustomBuildUntilLatestAliasIsReady(t *testing.T) {
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.156", Name: "v0.1.156"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient: &updateServiceTagClientStub{
				tags: []string{"0.1.156-xd.5"},
				manifestDigests: map[string]string{
					"0.1.156-xd.5": "sha256:exact",
					"latest":       "sha256:older",
				},
			},
			Updater: &updateServiceContainerUpdaterStub{configured: true},
		},
	)

	_, err := svc.PerformUpdate(context.Background())

	require.ErrorIs(t, err, ErrCustomUpdateNotReady)
}

func TestUpdateServicePerformUpdateRejectsUnconfiguredContainerUpdater(t *testing.T) {
	svc := NewUpdateServiceWithOptions(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.156", Name: "v0.1.156"},
		},
		"0.1.156-xd.4",
		"custom",
		UpdateServiceOptions{
			CustomRepo:  "xiao-dan-1/sub2api",
			CustomImage: "ghcr.io/xiao-dan-1/sub2api",
			TagClient:   &updateServiceTagClientStub{tags: []string{"0.1.156-xd.5"}},
			Updater:     &updateServiceContainerUpdaterStub{configured: false},
		},
	)

	_, err := svc.PerformUpdate(context.Background())

	require.ErrorIs(t, err, ErrCustomUpdaterUnavailable)
}

func TestUpdateServiceRollbackToVersionRejectsCustomBuild(t *testing.T) {
	svc := newRollbackTestService(
		"0.1.153-xd.1",
		[]*GitHubRelease{
			{TagName: "v0.1.153"},
			{TagName: "v0.1.152"},
		},
	)

	err := svc.RollbackToVersion(context.Background(), "0.1.152")

	require.ErrorIs(t, err, ErrCustomBuildOnlineUpdateDisabled)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}
