package common

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAddressesMaskOnlyConfiguredOriginsAndProxies(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://environment-proxy.internal:8080")
	t.Setenv("HTTPS_PROXY", "")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAdvancedCustom)
	SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://private-upstream.internal/api")
	SetContextKey(c, constant.ContextKeyUpstreamRequestOrigin, "wss://absolute-upstream.internal")
	SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{Proxy: "http://channel-proxy.internal:8081"})
	c.Set("pluginAllowedHosts", []string{"plugin-upstream.internal"})
	addresses := UpstreamAddresses(c)
	message := MaskSensitiveInfo("private-upstream.internal absolute-upstream.internal channel-proxy.internal environment-proxy.internal plugin-upstream.internal https://docs.example.org/errors mail@example.org grpc.retry.info", addresses...)
	for _, host := range []string{"private-upstream", "absolute-upstream", "channel-proxy", "environment-proxy", "plugin-upstream"} {
		require.NotContains(t, message, host)
	}
	require.Contains(t, message, "https://docs.example.org/errors")
	require.Contains(t, message, "mail@example.org")
	require.Contains(t, message, "grpc.retry.info")
	SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://api.openai.com")
	require.NotContains(t, UpstreamAddresses(c), "https://api.openai.com")
	require.NotContains(t, UpstreamAddresses(c), "wss://absolute-upstream.internal")
}
