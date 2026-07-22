package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCustomUpdateConfigFromEnv(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("UPDATE_CUSTOM_REPO", "xiao-dan-1/sub2api")
	t.Setenv("UPDATE_CUSTOM_IMAGE", "ghcr.io/xiao-dan-1/sub2api")
	t.Setenv("UPDATE_WATCHTOWER_URL", "http://watchtower:8080/v1/update")
	t.Setenv("UPDATE_WATCHTOWER_TOKEN", "test-watchtower-token")

	cfg, err := Load()

	require.NoError(t, err)
	require.Equal(t, "xiao-dan-1/sub2api", cfg.Update.CustomRepo)
	require.Equal(t, "ghcr.io/xiao-dan-1/sub2api", cfg.Update.CustomImage)
	require.Equal(t, "http://watchtower:8080/v1/update", cfg.Update.WatchtowerURL)
	require.Equal(t, "test-watchtower-token", cfg.Update.WatchtowerToken)
}
