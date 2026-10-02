package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelSyncTLSConfigurationIsIndependent(t *testing.T) {
	previous := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = true
	t.Cleanup(func() { common.TLSInsecureSkipVerify = previous })
	first := newHTTPClient()
	second := newHTTPClient()
	t.Cleanup(first.CloseIdleConnections)
	t.Cleanup(second.CloseIdleConnections)
	firstTransport, ok := first.Transport.(*http.Transport)
	require.True(t, ok)
	secondTransport, ok := second.Transport.(*http.Transport)
	require.True(t, ok)
	firstTransport.TLSClientConfig.InsecureSkipVerify = false
	firstTransport.TLSClientConfig.ServerName = "sync.example"
	assert.True(t, secondTransport.TLSClientConfig.InsecureSkipVerify)
	assert.Empty(t, secondTransport.TLSClientConfig.ServerName)
	assert.True(t, common.InsecureTLSConfig.InsecureSkipVerify)
	assert.Empty(t, common.InsecureTLSConfig.ServerName)
}
