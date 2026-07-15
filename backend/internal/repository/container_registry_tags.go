package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	ghcrHost               = "ghcr.io"
	registryTagsLimit      = 100
	maxRegistryTagPages    = 100
	registryErrorLimit     = 4 * 1024
	registryManifestAccept = "application/vnd.oci.image.index.v1+json, " +
		"application/vnd.oci.image.manifest.v1+json, " +
		"application/vnd.docker.distribution.manifest.list.v2+json, " +
		"application/vnd.docker.distribution.manifest.v2+json"
)

type containerRegistryTagClient struct {
	httpClient      *http.Client
	tokenBaseURL    string
	registryBaseURL string
	initErr         error
}

// NewContainerRegistryTagClient creates a proxy-aware client for public GHCR tags.
func NewContainerRegistryTagClient(proxyURL string, allowDirectOnProxyError bool) service.ContainerTagClient {
	client, err := httpclient.GetClient(httpclient.Options{
		Timeout:  30 * time.Second,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			return &containerRegistryTagClient{initErr: fmt.Errorf("container registry client init failed: %w", err)}
		}
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &containerRegistryTagClient{
		httpClient:      client,
		tokenBaseURL:    "https://ghcr.io",
		registryBaseURL: "https://ghcr.io",
	}
}

func newContainerRegistryTagClientForTest(client *http.Client, tokenBaseURL, registryBaseURL string) *containerRegistryTagClient {
	return &containerRegistryTagClient{
		httpClient:      client,
		tokenBaseURL:    strings.TrimRight(tokenBaseURL, "/"),
		registryBaseURL: strings.TrimRight(registryBaseURL, "/"),
	}
}

func (c *containerRegistryTagClient) ListTags(ctx context.Context, image string) ([]string, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	repository, err := parseGHCRRepository(image)
	if err != nil {
		return nil, err
	}

	token, err := c.fetchPullToken(ctx, repository)
	if err != nil {
		return nil, err
	}

	nextURL := fmt.Sprintf("%s/v2/%s/tags/list?n=%d", c.registryBaseURL, escapeRepositoryPath(repository), registryTagsLimit)
	allTags := make([]string, 0, registryTagsLimit)
	for page := 0; nextURL != "" && page < maxRegistryTagPages; page++ {
		tags, followingURL, err := c.fetchTagPage(ctx, nextURL, token)
		if err != nil {
			return nil, err
		}
		allTags = append(allTags, tags...)
		nextURL = followingURL
	}
	if nextURL != "" {
		return nil, fmt.Errorf("container registry tag pagination exceeded %d pages", maxRegistryTagPages)
	}
	return allTags, nil
}

func (c *containerRegistryTagClient) ManifestDigest(ctx context.Context, image, reference string) (string, error) {
	if c.initErr != nil {
		return "", c.initErr
	}
	repository, err := parseGHCRRepository(image)
	if err != nil {
		return "", err
	}
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.ContainsAny(reference, "/?#\\") {
		return "", fmt.Errorf("invalid container manifest reference")
	}

	token, err := c.fetchPullToken(ctx, repository)
	if err != nil {
		return "", err
	}
	manifestURL := fmt.Sprintf(
		"%s/v2/%s/manifests/%s",
		c.registryBaseURL,
		escapeRepositoryPath(repository),
		url.PathEscape(reference),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", registryManifestAccept)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Sub2API-Updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", registryHTTPError("manifest", resp)
	}
	digest := strings.TrimSpace(resp.Header.Get("Docker-Content-Digest"))
	if digest == "" {
		return "", fmt.Errorf("container registry manifest response is missing Docker-Content-Digest")
	}
	return digest, nil
}

func (c *containerRegistryTagClient) fetchTagPage(ctx context.Context, tagsURL, token string) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Sub2API-Updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", registryHTTPError("tag list", resp)
	}

	var payload struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, "", fmt.Errorf("decode container tags: %w", err)
	}
	nextURL, err := registryNextURL(resp.Header.Get("Link"), c.registryBaseURL)
	if err != nil {
		return nil, "", err
	}
	return payload.Tags, nextURL, nil
}

func registryNextURL(linkHeader, registryBaseURL string) (string, error) {
	for _, link := range strings.Split(linkHeader, ",") {
		parts := strings.Split(link, ";")
		if len(parts) < 2 || !strings.Contains(strings.ToLower(strings.Join(parts[1:], ";")), `rel="next"`) {
			continue
		}
		target := strings.TrimSpace(parts[0])
		if len(target) < 3 || target[0] != '<' || target[len(target)-1] != '>' {
			return "", fmt.Errorf("invalid container registry pagination link")
		}
		base, err := url.Parse(registryBaseURL)
		if err != nil {
			return "", err
		}
		reference, err := url.Parse(target[1 : len(target)-1])
		if err != nil {
			return "", fmt.Errorf("parse container registry pagination link: %w", err)
		}
		resolved := base.ResolveReference(reference)
		if !strings.EqualFold(resolved.Scheme, base.Scheme) || !strings.EqualFold(resolved.Host, base.Host) || !strings.HasPrefix(resolved.Path, "/v2/") {
			return "", fmt.Errorf("container registry pagination link changed origin")
		}
		return resolved.String(), nil
	}
	return "", nil
}

func (c *containerRegistryTagClient) fetchPullToken(ctx context.Context, repository string) (string, error) {
	tokenURL, err := url.Parse(c.tokenBaseURL + "/token")
	if err != nil {
		return "", err
	}
	query := tokenURL.Query()
	query.Set("service", ghcrHost)
	query.Set("scope", "repository:"+repository+":pull")
	tokenURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-Updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", registryHTTPError("token", resp)
	}

	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode container registry token: %w", err)
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		token = strings.TrimSpace(payload.AccessToken)
	}
	if token == "" {
		return "", fmt.Errorf("container registry returned an empty pull token")
	}
	return token, nil
}

func parseGHCRRepository(image string) (string, error) {
	raw := strings.TrimSpace(image)
	if raw == "" {
		return "", fmt.Errorf("custom container image is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse custom container image: %w", err)
	}
	if !strings.EqualFold(parsed.Hostname(), ghcrHost) {
		return "", fmt.Errorf("custom container image must use %s", ghcrHost)
	}
	repository := strings.Trim(parsed.Path, "/")
	if repository == "" || strings.ContainsAny(repository, "@:#?\\") {
		return "", fmt.Errorf("custom container image must be an untagged ghcr.io repository")
	}
	return repository, nil
}

func escapeRepositoryPath(repository string) string {
	parts := strings.Split(repository, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func registryHTTPError(operation string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, registryErrorLimit))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("container registry %s returned %d", operation, resp.StatusCode)
	}
	return fmt.Errorf("container registry %s returned %d: %s", operation, resp.StatusCode, detail)
}
