package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerRegistryTagClientListsGHCRTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "ghcr.io", r.URL.Query().Get("service"))
			require.Equal(t, "repository:xiao-dan-1/sub2api:pull", r.URL.Query().Get("scope"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
		case "/v2/xiao-dan-1/sub2api/tags/list":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "100", r.URL.Query().Get("n"))
			require.Equal(t, "Bearer registry-token", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"xiao-dan-1/sub2api","tags":["latest","0.1.156-xd.4"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	tags, err := client.ListTags(context.Background(), "ghcr.io/xiao-dan-1/sub2api")

	require.NoError(t, err)
	require.Equal(t, []string{"latest", "0.1.156-xd.4"}, tags)
}

func TestContainerRegistryTagClientRejectsUnsupportedHost(t *testing.T) {
	client := newContainerRegistryTagClientForTest(http.DefaultClient, "https://ghcr.io", "https://ghcr.io")

	_, err := client.ListTags(context.Background(), "docker.io/example/sub2api")

	require.Error(t, err)
	require.Contains(t, err.Error(), "ghcr.io")
}

func TestContainerRegistryTagClientReturnsTokenFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	_, err := client.ListTags(context.Background(), "ghcr.io/xiao-dan-1/sub2api")

	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

func TestContainerRegistryTagClientReturnsTagListFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	_, err := client.ListTags(context.Background(), "ghcr.io/xiao-dan-1/sub2api")

	require.Error(t, err)
	require.Contains(t, err.Error(), "503")
}

func TestContainerRegistryTagClientFollowsTagPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
		case "/v2/xiao-dan-1/sub2api/tags/list":
			if r.URL.Query().Get("last") == "0.1.156-xd.4" {
				_, _ = w.Write([]byte(`{"tags":["0.1.157-xd.1"]}`))
				return
			}
			w.Header().Set("Link", `</v2/xiao-dan-1/sub2api/tags/list?n=100&last=0.1.156-xd.4>; rel="next"`)
			_, _ = w.Write([]byte(`{"tags":["0.1.156-xd.4"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	tags, err := client.ListTags(context.Background(), "ghcr.io/xiao-dan-1/sub2api")

	require.NoError(t, err)
	require.Equal(t, []string{"0.1.156-xd.4", "0.1.157-xd.1"}, tags)
}

func TestContainerRegistryTagClientReadsManifestDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.Equal(t, "repository:xiao-dan-1/sub2api:pull", r.URL.Query().Get("scope"))
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
		case "/v2/xiao-dan-1/sub2api/manifests/v0.1.156-xd.5":
			require.Equal(t, http.MethodHead, r.Method)
			require.Equal(t, "Bearer registry-token", r.Header.Get("Authorization"))
			require.Contains(t, r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json")
			require.Contains(t, r.Header.Get("Accept"), "application/vnd.docker.distribution.manifest.list.v2+json")
			w.Header().Set("Docker-Content-Digest", "sha256:manifest-digest")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	digest, err := client.ManifestDigest(
		context.Background(),
		"ghcr.io/xiao-dan-1/sub2api",
		"v0.1.156-xd.5",
	)

	require.NoError(t, err)
	require.Equal(t, "sha256:manifest-digest", digest)
}

func TestContainerRegistryTagClientRejectsMissingManifestDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	_, err := client.ManifestDigest(
		context.Background(),
		"ghcr.io/xiao-dan-1/sub2api",
		"0.1.156-xd.5",
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "Docker-Content-Digest")
}

func TestContainerRegistryTagClientReturnsManifestFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
			return
		}
		http.Error(w, "manifest unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newContainerRegistryTagClientForTest(server.Client(), server.URL, server.URL)

	_, err := client.ManifestDigest(
		context.Background(),
		"ghcr.io/xiao-dan-1/sub2api",
		"0.1.156-xd.5",
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "503")
	require.Contains(t, err.Error(), "manifest")
}
