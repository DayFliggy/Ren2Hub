package service

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPTransportsKeepIndependentTLSSettings(t *testing.T) {
	previous := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = true
	t.Cleanup(func() { common.TLSInsecureSkipVerify = previous })
	protected := &ssrfProtectedRoundTripper{}
	transports := []*http.Transport{
		newRelayHTTPTransport(),
		newRelayHTTPTransport(),
		protected.newTransport(nil),
		protected.newTransport(&url.URL{Scheme: "http", Host: "127.0.0.1:8080"}),
	}
	for _, transport := range transports {
		t.Cleanup(transport.CloseIdleConnections)
		require.NotNil(t, transport.TLSClientConfig)
		assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	}
	transports[0].TLSClientConfig.ServerName = "relay.example"
	transports[0].TLSClientConfig.MinVersion = tls.VersionTLS13
	transports[0].TLSClientConfig.InsecureSkipVerify = false
	for _, transport := range transports[1:] {
		assert.Empty(t, transport.TLSClientConfig.ServerName)
		assert.Equal(t, common.InsecureTLSConfig.MinVersion, transport.TLSClientConfig.MinVersion)
		assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	}
	assert.Empty(t, common.InsecureTLSConfig.ServerName)
	assert.True(t, common.InsecureTLSConfig.InsecureSkipVerify)
}
