package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	watchtowerRequestTimeout = 10 * time.Minute
	watchtowerErrorLimit     = 4 * 1024
)

type watchtowerClient struct {
	endpoint   string
	token      string
	httpClient *http.Client
	configErr  error
}

// NewWatchtowerClient creates a client for Watchtower's authenticated HTTP API.
func NewWatchtowerClient(endpoint, token string) service.ContainerUpdater {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return newWatchtowerClientForTest(endpoint, token, &http.Client{
		Transport: transport,
		Timeout:   watchtowerRequestTimeout,
	})
}

func newWatchtowerClientForTest(endpoint, token string, client *http.Client) *watchtowerClient {
	result := &watchtowerClient{
		endpoint:   strings.TrimSpace(endpoint),
		token:      strings.TrimSpace(token),
		httpClient: client,
	}
	if result.endpoint == "" || result.token == "" {
		result.configErr = fmt.Errorf("Watchtower endpoint and API token are required")
		return result
	}
	parsed, err := url.Parse(result.endpoint)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		result.configErr = fmt.Errorf("invalid Watchtower endpoint")
	}
	return result
}

func (c *watchtowerClient) Configured() bool {
	return c != nil && c.configErr == nil && c.httpClient != nil
}

func (c *watchtowerClient) TriggerUpdate(ctx context.Context) error {
	if !c.Configured() {
		if c == nil || c.configErr == nil {
			return fmt.Errorf("Watchtower client is not configured")
		}
		return c.configErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-Updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("trigger Watchtower update: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, watchtowerErrorLimit))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("Watchtower update returned %d", resp.StatusCode)
	}
	return fmt.Errorf("Watchtower update returned %d: %s", resp.StatusCode, detail)
}
