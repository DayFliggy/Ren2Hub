package service

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyImageURLCapability(t *testing.T) {
	previousSecret := common.SessionSecret
	common.SessionSecret = "midjourney-image-test-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })

	now := time.Unix(1_700_000_000, 0)
	expiresAt := now.Add(time.Hour).Unix()
	signature := MidjourneyImageURLSignature("mj-task-1", expiresAt)

	require.True(t, ValidateMidjourneyImageURL("mj-task-1", expiresAt, signature, now))
	require.False(t, ValidateMidjourneyImageURL("mj-task-2", expiresAt, signature, now))
	require.False(t, ValidateMidjourneyImageURL("mj-task-1", expiresAt, signature, now.Add(time.Hour)))
	require.False(t, ValidateMidjourneyImageURL("mj-task-1", expiresAt, "", now))
}

func TestBuildMidjourneyImageURLContainsEscapedCapability(t *testing.T) {
	previousSecret := common.SessionSecret
	common.SessionSecret = "midjourney-image-test-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })

	imageURL := BuildMidjourneyImageURL("https://example.test/", "mj-task-1")
	parsed, err := url.Parse(imageURL)
	require.NoError(t, err)
	require.Equal(t, "/mj/image/mj-task-1", parsed.Path)
	expiresAt, err := strconv.ParseInt(parsed.Query().Get("exp"), 10, 64)
	require.NoError(t, err)
	require.True(t, ValidateMidjourneyImageURL("mj-task-1", expiresAt, parsed.Query().Get("sig"), time.Now()))
}
