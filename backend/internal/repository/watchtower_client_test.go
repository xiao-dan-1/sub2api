package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewWatchtowerClientHandlesCustomDefaultTransport(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	require.NotPanics(t, func() {
		client := NewWatchtowerClient("http://watchtower:8080/v1/update", "test-token")
		require.True(t, client.Configured())
	})
}

func TestWatchtowerClientTriggersAuthenticatedUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/update", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newWatchtowerClientForTest(server.URL+"/v1/update", "test-token", server.Client())

	require.True(t, client.Configured())
	require.NoError(t, client.TriggerUpdate(context.Background()))
}

func TestWatchtowerClientReportsMissingConfiguration(t *testing.T) {
	client := NewWatchtowerClient("", "")

	require.False(t, client.Configured())
	require.Error(t, client.TriggerUpdate(context.Background()))
}

func TestWatchtowerClientReturnsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newWatchtowerClientForTest(server.URL+"/v1/update", "test-token", server.Client())

	err := client.TriggerUpdate(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "bad token")
}

func TestWatchtowerClientRejectsInvalidEndpoint(t *testing.T) {
	client := NewWatchtowerClient("://bad-url", "test-token")

	require.False(t, client.Configured())
	require.Error(t, client.TriggerUpdate(context.Background()))
}
