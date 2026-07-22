package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrCustomUpdateNotReady     = infraerrors.Conflict("CUSTOM_UPDATE_NOT_READY", "the matching custom container image is not ready yet")
	ErrCustomUpdaterUnavailable = infraerrors.BadRequest("CUSTOM_UPDATER_UNAVAILABLE", "the custom container updater is not configured")
)

const customUpdateTriggerTimeout = 10 * time.Minute

// ContainerTagClient lists published tags for a container image repository.
type ContainerTagClient interface {
	ListTags(ctx context.Context, image string) ([]string, error)
	ManifestDigest(ctx context.Context, image, reference string) (string, error)
}

// ContainerUpdater triggers replacement of the running container.
type ContainerUpdater interface {
	Configured() bool
	TriggerUpdate(ctx context.Context) error
}

// CustomImageUpdateServiceOptions configures the fork-owned container update path.
type CustomImageUpdateServiceOptions struct {
	CustomRepo  string
	CustomImage string
	TagClient   ContainerTagClient
	Updater     ContainerUpdater
}

// CustomImageUpdateService checks published fork images and triggers Watchtower.
type CustomImageUpdateService struct {
	currentVersion string
	customRepo     string
	customImage    string
	tagClient      ContainerTagClient
	updater        ContainerUpdater
}

// CustomImageUpdateInfo describes whether a verified custom image can replace the running container.
type CustomImageUpdateInfo struct {
	CurrentVersion   string `json:"current_version"`
	TargetVersion    string `json:"target_version,omitempty"`
	Image            string `json:"image,omitempty"`
	TargetImage      string `json:"target_image,omitempty"`
	TargetDigest     string `json:"target_digest,omitempty"`
	ReleaseURL       string `json:"release_url,omitempty"`
	HasUpdate        bool   `json:"has_update"`
	TargetReady      bool   `json:"target_ready"`
	LatestAliasReady bool   `json:"latest_alias_ready"`
	Ready            bool   `json:"ready"`
	Warning          string `json:"warning,omitempty"`
}

// CustomImageUpdateResult is returned after Watchtower accepts the bounded trigger request.
type CustomImageUpdateResult struct {
	TargetVersion    string `json:"target_version"`
	TargetImage      string `json:"target_image"`
	TargetDigest     string `json:"target_digest"`
	AutomaticRestart bool   `json:"automatic_restart"`
}

// NewCustomImageUpdateService creates an isolated custom image updater.
func NewCustomImageUpdateService(version string, options CustomImageUpdateServiceOptions) *CustomImageUpdateService {
	return &CustomImageUpdateService{
		currentVersion: strings.TrimSpace(version),
		customRepo:     strings.Trim(strings.TrimSpace(options.CustomRepo), "/"),
		customImage:    strings.TrimSpace(options.CustomImage),
		tagClient:      options.TagClient,
		updater:        options.Updater,
	}
}

// Check discovers the greatest valid custom release and verifies its exact tag and latest alias.
func (s *CustomImageUpdateService) Check(ctx context.Context) (*CustomImageUpdateInfo, error) {
	info := &CustomImageUpdateInfo{
		CurrentVersion: s.currentVersion,
		Image:          s.customImage,
	}
	if s.customImage == "" {
		info.Warning = "custom container image is not configured"
		return info, nil
	}
	if s.tagClient == nil {
		info.Warning = "custom container image discovery is not configured"
		return info, nil
	}

	tags, err := s.tagClient.ListTags(ctx, s.customImage)
	if err != nil {
		info.Warning = "failed to inspect custom container image: " + err.Error()
		return info, nil
	}

	target, ok := selectHighestCustomImagePackage(tags)
	if !ok {
		info.Warning = "no valid custom container image release is published"
		return info, nil
	}

	info.TargetVersion = target.Version
	info.TargetImage = customImageReference(s.customImage, target.Tag)
	info.ReleaseURL = customReleaseURL(s.customRepo, target.Version)
	info.HasUpdate = compareCustomVersions(s.currentVersion, target.Version) < 0
	if !info.HasUpdate {
		return info, nil
	}

	targetDigest, err := s.tagClient.ManifestDigest(ctx, s.customImage, target.Tag)
	if err != nil {
		info.Warning = "failed to inspect exact custom container image: " + err.Error()
		return info, nil
	}
	info.TargetDigest = strings.TrimSpace(targetDigest)
	if info.TargetDigest == "" {
		info.Warning = "exact custom container image is not ready"
		return info, nil
	}
	info.TargetReady = true

	latestDigest, err := s.tagClient.ManifestDigest(ctx, s.customImage, "latest")
	if err != nil {
		info.Warning = "failed to inspect custom container image latest tag: " + err.Error()
		return info, nil
	}
	if !strings.EqualFold(info.TargetDigest, strings.TrimSpace(latestDigest)) {
		info.Warning = fmt.Sprintf("custom container image latest tag does not yet match %s", target.Version)
		return info, nil
	}
	info.LatestAliasReady = true

	if !s.updaterConfigured() {
		info.Warning = "custom container updater is not configured"
		return info, nil
	}
	info.Ready = true
	return info, nil
}

// Trigger re-checks registry readiness and waits for Watchtower using a request-independent timeout.
func (s *CustomImageUpdateService) Trigger(ctx context.Context) (*CustomImageUpdateResult, error) {
	triggerCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), customUpdateTriggerTimeout)
	defer cancel()

	info, err := s.Check(triggerCtx)
	if err != nil {
		return nil, err
	}
	if !info.HasUpdate {
		return nil, ErrNoUpdateAvailable
	}
	if !s.updaterConfigured() {
		return nil, ErrCustomUpdaterUnavailable
	}
	if !info.Ready {
		return nil, ErrCustomUpdateNotReady
	}
	if err := s.updater.TriggerUpdate(triggerCtx); err != nil {
		return nil, fmt.Errorf("trigger custom container update: %w", err)
	}

	return &CustomImageUpdateResult{
		TargetVersion:    info.TargetVersion,
		TargetImage:      info.TargetImage,
		TargetDigest:     info.TargetDigest,
		AutomaticRestart: true,
	}, nil
}

func (s *CustomImageUpdateService) updaterConfigured() bool {
	return s.updater != nil && s.updater.Configured()
}

func selectHighestCustomImagePackage(tags []string) (customPackage, bool) {
	best := customPackage{}
	for _, tag := range tags {
		core, revision, ok := parseCustomVersion(tag)
		if !ok {
			continue
		}
		candidate := customPackage{
			Version: fmt.Sprintf("%s-xd.%d", core, revision),
			Tag:     strings.TrimSpace(tag),
		}
		if best.Version == "" || compareCustomVersions(best.Version, candidate.Version) < 0 {
			best = candidate
		}
	}
	return best, best.Version != ""
}

type customPackage struct {
	Version string
	Tag     string
}

func parseCustomVersion(value string) (string, int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, "-xd.")
	if len(parts) != 2 || !isStrictVersionCore(parts[0]) || parts[1] == "" {
		return "", 0, false
	}
	revision, err := strconv.Atoi(parts[1])
	if err != nil || revision < 0 || strconv.Itoa(revision) != parts[1] {
		return "", 0, false
	}
	return parts[0], revision, true
}

func isStrictVersionCore(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func compareCustomVersions(current, target string) int {
	if coreComparison := compareVersions(versionCore(current), versionCore(target)); coreComparison != 0 {
		return coreComparison
	}
	_, currentRevision, currentOK := parseCustomVersion(current)
	_, targetRevision, targetOK := parseCustomVersion(target)
	if !targetOK {
		return 0
	}
	if !currentOK {
		currentRevision = -1
	}
	switch {
	case currentRevision < targetRevision:
		return -1
	case currentRevision > targetRevision:
		return 1
	default:
		return 0
	}
}

func versionCore(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if suffixIndex := strings.IndexAny(value, "-+"); suffixIndex >= 0 {
		value = value[:suffixIndex]
	}
	return value
}

func customReleaseURL(repo, version string) string {
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(version) == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/releases/tag/v%s", strings.Trim(repo, "/ "), version)
}

func customImageReference(image, tag string) string {
	image = strings.TrimSpace(image)
	tag = strings.TrimSpace(tag)
	if image == "" || tag == "" {
		return ""
	}
	return image + ":" + tag
}
