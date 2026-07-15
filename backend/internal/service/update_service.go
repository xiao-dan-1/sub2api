package service

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrNoUpdateAvailable               = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed       = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
	ErrCustomBuildOnlineUpdateDisabled = infraerrors.BadRequest("CUSTOM_BUILD_ONLINE_UPDATE_DISABLED", "online update is disabled for custom builds; update from your fork/custom image")
	ErrCustomUpdateNotReady            = infraerrors.Conflict("CUSTOM_UPDATE_NOT_READY", "the matching custom container image is not ready yet")
	ErrCustomUpdaterUnavailable        = infraerrors.BadRequest("CUSTOM_UPDATER_UNAVAILABLE", "the custom container updater is not configured")
)

const (
	updateCacheKey = "update_check_cache"
	updateCacheTTL = 1200 // 20 minutes
	githubRepo     = "Wei-Shaw/sub2api"

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 15

	customUpdateTriggerTimeout = 10 * time.Minute
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

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

// UpdateServiceOptions configures the custom container update path.
type UpdateServiceOptions struct {
	CustomRepo  string
	CustomImage string
	TagClient   ContainerTagClient
	Updater     ContainerUpdater
}

// UpdateService handles software updates
type UpdateService struct {
	cache          UpdateCache
	githubClient   GitHubReleaseClient
	currentVersion string
	buildType      string // "source" for manual builds, "release" for CI builds, "custom" for forked builds
	customRepo     string
	customImage    string
	tagClient      ContainerTagClient
	updater        ContainerUpdater
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return NewUpdateServiceWithOptions(cache, githubClient, version, buildType, UpdateServiceOptions{})
}

// NewUpdateServiceWithOptions creates an UpdateService with custom container update support.
func NewUpdateServiceWithOptions(
	cache UpdateCache,
	githubClient GitHubReleaseClient,
	version string,
	buildType string,
	options UpdateServiceOptions,
) *UpdateService {
	return &UpdateService{
		cache:          cache,
		githubClient:   githubClient,
		currentVersion: version,
		buildType:      buildType,
		customRepo:     strings.TrimSpace(options.CustomRepo),
		customImage:    strings.TrimSpace(options.CustomImage),
		tagClient:      options.TagClient,
		updater:        options.Updater,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion        string       `json:"current_version"`
	LatestVersion         string       `json:"latest_version"`
	HasUpdate             bool         `json:"has_update"`
	ReleaseInfo           *ReleaseInfo `json:"release_info,omitempty"`
	Cached                bool         `json:"cached"`
	Warning               string       `json:"warning,omitempty"`
	BuildType             string       `json:"build_type"` // "source", "release", or "custom"
	CustomVersion         string       `json:"custom_version,omitempty"`
	CustomImage           string       `json:"custom_image,omitempty"`
	CustomReleaseURL      string       `json:"custom_release_url,omitempty"`
	CustomUpdateAvailable bool         `json:"custom_update_available"`
	CustomUpdateReady     bool         `json:"custom_update_ready"`
	CustomUpdateWarning   string       `json:"custom_update_warning,omitempty"`
}

// UpdateExecutionResult describes what the caller must do after an update.
type UpdateExecutionResult struct {
	NeedRestart      bool   `json:"need_restart"`
	AutomaticRestart bool   `json:"automatic_restart"`
	TargetVersion    string `json:"target_version,omitempty"`
	TargetImage      string `json:"target_image,omitempty"`
}

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	// Try cache first
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	// Fetch from GitHub
	info, err := s.fetchLatestRelease(ctx)
	if err != nil {
		// Return cached on error
		if cached, cacheErr := s.getFromCache(ctx); cacheErr == nil && cached != nil {
			cached.Warning = "Using cached data: " + err.Error()
			return cached, nil
		}
		info := &UpdateInfo{
			CurrentVersion: s.currentVersion,
			LatestVersion:  s.currentVersion,
			HasUpdate:      false,
			Warning:        err.Error(),
			BuildType:      s.effectiveBuildType(),
		}
		if s.isCustomBuild() {
			info.CustomImage = s.customImage
		}
		return info, nil
	}

	// Cache result
	s.saveToCache(ctx, info)
	return info, nil
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) (*UpdateExecutionResult, error) {
	if s.isCustomBuild() {
		return s.performCustomContainerUpdate(ctx)
	}

	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return nil, err
	}

	if !info.HasUpdate {
		return nil, ErrNoUpdateAvailable
	}

	if err := s.applyReleaseAssets(ctx, info.ReleaseInfo.Assets); err != nil {
		return nil, err
	}
	return &UpdateExecutionResult{NeedRestart: true}, nil
}

func (s *UpdateService) performCustomContainerUpdate(ctx context.Context) (*UpdateExecutionResult, error) {
	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return nil, err
	}
	if !info.CustomUpdateAvailable {
		if info.HasUpdate {
			return nil, ErrCustomUpdateNotReady
		}
		return nil, ErrNoUpdateAvailable
	}
	if !s.updaterConfigured() {
		return nil, ErrCustomUpdaterUnavailable
	}
	if !info.CustomUpdateReady {
		return nil, ErrCustomUpdateNotReady
	}

	targetImage := latestImageReference(s.customImage)
	triggerCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), customUpdateTriggerTimeout)
	defer cancel()
	if err := s.updater.TriggerUpdate(triggerCtx); err != nil {
		return nil, fmt.Errorf("trigger custom container update: %w", err)
	}

	return &UpdateExecutionResult{
		NeedRestart:      false,
		AutomaticRestart: true,
		TargetVersion:    info.CustomVersion,
		TargetImage:      targetImage,
	}, nil
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, releaseAssets []Asset) error {
	// Find matching archive and checksum for current platform
	archiveName := s.getArchiveName()
	var downloadURL string
	var checksumURL string

	for _, asset := range releaseAssets {
		if strings.Contains(asset.Name, archiveName) && !strings.HasSuffix(asset.Name, ".txt") {
			downloadURL = asset.DownloadURL
		}
		if asset.Name == "checksums.txt" {
			checksumURL = asset.DownloadURL
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// SECURITY: Validate download URL is from trusted domain
	if err := validateDownloadURL(downloadURL); err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if checksumURL != "" {
		if err := validateDownloadURL(checksumURL); err != nil {
			return fmt.Errorf("invalid checksum URL: %w", err)
		}
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, err := os.MkdirTemp(exeDir, ".sub2api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Download archive
	archivePath := filepath.Join(tempDir, filepath.Base(downloadURL))
	if err := s.downloadFile(ctx, downloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify checksum if available
	if checksumURL != "" {
		if err := s.verifyChecksum(ctx, archivePath, checksumURL); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, "sub2api")
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 fails, restore backup
	backupPath := exePath + ".backup"

	// Remove old backup if exists
	_ = os.Remove(backupPath)

	// Step 1: Move current binary to backup
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Step 2: Move new binary to target location (atomic, same filesystem)
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("no backup found")
	}

	// Replace current with backup
	if err := os.Rename(backupFile, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	if s.isCustomBuild() {
		return ErrCustomBuildOnlineUpdateDisabled
	}

	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.githubClient.FetchRecentReleases(ctx, githubRepo, rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		if v == "" || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) fetchLatestRelease(ctx context.Context) (*UpdateInfo, error) {
	release, err := s.githubClient.FetchLatestRelease(ctx, githubRepo)
	if err != nil {
		return nil, err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	assets := make([]Asset, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	info := &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  latestVersion,
		HasUpdate:      compareVersions(s.currentVersion, latestVersion) < 0,
		ReleaseInfo: &ReleaseInfo{
			Name:        release.Name,
			Body:        release.Body,
			PublishedAt: release.PublishedAt,
			HTMLURL:     release.HTMLURL,
			Assets:      assets,
		},
		Cached:    false,
		Warning:   s.customBuildWarning(),
		BuildType: s.effectiveBuildType(),
	}
	if s.isCustomBuild() {
		s.enrichCustomUpdate(ctx, info)
	}
	return info, nil
}

func (s *UpdateService) enrichCustomUpdate(ctx context.Context, info *UpdateInfo) {
	info.CustomImage = s.customImage
	if s.tagClient == nil || s.customImage == "" {
		info.CustomUpdateWarning = "custom container image discovery is not configured"
		return
	}

	tags, err := s.tagClient.ListTags(ctx, s.customImage)
	if err != nil {
		info.CustomUpdateWarning = "failed to inspect custom container image: " + err.Error()
		return
	}

	target, ok := selectHighestCustomPackage(tags, info.LatestVersion)
	if !ok {
		if compareVersions(s.currentVersion, info.LatestVersion) < 0 {
			info.CustomUpdateWarning = fmt.Sprintf("waiting for custom container image matching %s", info.LatestVersion)
		}
		return
	}

	info.CustomVersion = target.Version
	info.CustomReleaseURL = customReleaseURL(s.customRepo, target.Version)
	info.CustomUpdateAvailable = compareCustomVersions(s.currentVersion, target.Version) < 0
	info.HasUpdate = info.HasUpdate || info.CustomUpdateAvailable
	if !info.CustomUpdateAvailable {
		return
	}

	targetDigest, err := s.tagClient.ManifestDigest(ctx, s.customImage, target.Tag)
	if err != nil {
		info.CustomUpdateWarning = "failed to inspect exact custom container image: " + err.Error()
		return
	}
	latestDigest, err := s.tagClient.ManifestDigest(ctx, s.customImage, "latest")
	if err != nil {
		info.CustomUpdateWarning = "failed to inspect custom container image latest tag: " + err.Error()
		return
	}
	if !strings.EqualFold(strings.TrimSpace(targetDigest), strings.TrimSpace(latestDigest)) {
		info.CustomUpdateWarning = fmt.Sprintf("custom container image latest tag does not yet match %s", target.Version)
		return
	}
	if !s.updaterConfigured() {
		info.CustomUpdateWarning = "custom container updater is not configured"
		return
	}
	info.CustomUpdateReady = true
}

type customPackage struct {
	Version string
	Tag     string
}

func selectHighestCustomPackage(tags []string, upstreamVersion string) (customPackage, bool) {
	targetCore := versionCore(upstreamVersion)
	bestRevision := -1
	best := customPackage{}
	for _, tag := range tags {
		core, revision, ok := parseCustomVersion(tag)
		if !ok || core != targetCore || revision <= bestRevision {
			continue
		}
		bestRevision = revision
		best = customPackage{
			Version: fmt.Sprintf("%s-xd.%d", core, revision),
			Tag:     strings.TrimSpace(tag),
		}
	}
	return best, best.Version != ""
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
	if coreComparison := compareVersions(current, target); coreComparison != 0 {
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

func latestImageReference(image string) string {
	image = strings.TrimSpace(image)
	if image == "" || strings.HasSuffix(image, ":latest") {
		return image
	}
	return image + ":latest"
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func (s *UpdateService) getArchiveName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s_%s", osName, arch)
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find expected hash in checksums file
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("checksum not found for %s", fileName)
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				continue // Skip directories and special files
			}

			// Only extract the specific binary we need
			if baseName == "sub2api" || baseName == "sub2api.exe" {
				// Additional security: limit file size (max 500MB)
				const maxBinarySize = 500 * 1024 * 1024
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				// Use LimitReader to prevent decompression bombs
				limited := io.LimitReader(tr, maxBinarySize)
				if _, err := io.Copy(out, limited); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("binary not found in archive")
	}

	// Direct copy for non-tar files (with size limit)
	const maxBinarySize = 500 * 1024 * 1024
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize)
	if _, err := io.Copy(out, limited); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Latest              string       `json:"latest"`
		ReleaseInfo         *ReleaseInfo `json:"release_info"`
		CustomVersion       string       `json:"custom_version"`
		CustomImage         string       `json:"custom_image"`
		CustomReleaseURL    string       `json:"custom_release_url"`
		CustomUpdateReady   bool         `json:"custom_update_ready"`
		CustomUpdateWarning string       `json:"custom_update_warning"`
		Timestamp           int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}

	if time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	customUpdateAvailable := cached.CustomVersion != "" && compareCustomVersions(s.currentVersion, cached.CustomVersion) < 0
	return &UpdateInfo{
		CurrentVersion:        s.currentVersion,
		LatestVersion:         cached.Latest,
		HasUpdate:             compareVersions(s.currentVersion, cached.Latest) < 0 || customUpdateAvailable,
		ReleaseInfo:           cached.ReleaseInfo,
		Cached:                true,
		Warning:               s.customBuildWarning(),
		BuildType:             s.effectiveBuildType(),
		CustomVersion:         cached.CustomVersion,
		CustomImage:           cached.CustomImage,
		CustomReleaseURL:      cached.CustomReleaseURL,
		CustomUpdateAvailable: customUpdateAvailable,
		CustomUpdateReady:     customUpdateAvailable && cached.CustomUpdateReady && s.updaterConfigured(),
		CustomUpdateWarning:   cached.CustomUpdateWarning,
	}, nil
}

func (s *UpdateService) isCustomBuild() bool {
	return strings.EqualFold(strings.TrimSpace(s.buildType), "custom") ||
		hasCustomVersionSuffix(s.currentVersion)
}

func (s *UpdateService) effectiveBuildType() string {
	if s.isCustomBuild() {
		return "custom"
	}
	return s.buildType
}

func (s *UpdateService) customBuildWarning() string {
	if !s.isCustomBuild() {
		return ""
	}
	return "custom build detected; web updates use the configured custom container image"
}

func (s *UpdateService) updaterConfigured() bool {
	return s.updater != nil && s.updater.Configured()
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Latest              string       `json:"latest"`
		ReleaseInfo         *ReleaseInfo `json:"release_info"`
		CustomVersion       string       `json:"custom_version"`
		CustomImage         string       `json:"custom_image"`
		CustomReleaseURL    string       `json:"custom_release_url"`
		CustomUpdateReady   bool         `json:"custom_update_ready"`
		CustomUpdateWarning string       `json:"custom_update_warning"`
		Timestamp           int64        `json:"timestamp"`
	}{
		Latest:              info.LatestVersion,
		ReleaseInfo:         info.ReleaseInfo,
		CustomVersion:       info.CustomVersion,
		CustomImage:         info.CustomImage,
		CustomReleaseURL:    info.CustomReleaseURL,
		CustomUpdateReady:   info.CustomUpdateReady,
		CustomUpdateWarning: info.CustomUpdateWarning,
		Timestamp:           time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares the numeric major.minor.patch version core.
// Custom build suffixes should not make the same upstream base look outdated.
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if suffixIndex := strings.IndexAny(v, "-+"); suffixIndex >= 0 {
		v = v[:suffixIndex]
	}
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}

func hasCustomVersionSuffix(v string) bool {
	v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
	suffixIndex := strings.Index(v, "-")
	if suffixIndex < 0 {
		return false
	}
	suffix := v[suffixIndex+1:]
	return strings.HasPrefix(suffix, "xd") || strings.HasPrefix(suffix, "custom")
}
