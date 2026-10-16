package vertex

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceAccountTokenExchangeVerifiesDespiteChannelTLSSkip(t *testing.T) {
	var exchanges atomic.Int32
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		exchanges.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"fixture-token"}`))
	}))
	t.Cleanup(tokenServer.Close)
	previousURL := googleOAuthTokenURL
	googleOAuthTokenURL = tokenServer.URL
	t.Cleanup(func() { googleOAuthTokenURL = previousURL })

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	adaptor := &Adaptor{AccountCredentials: Credentials{
		ClientEmail: "fixture@example.iam.gserviceaccount.com",
		PrivateKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})),
	}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId:      1,
		ApiKey:         "tls-skip-service-account-fixture",
		ChannelSetting: dto.ChannelSettings{TLSInsecureSkipVerify: true},
	}}

	_, err = getAccessToken(context.Background(), adaptor, info)
	var verifyErr *tls.CertificateVerificationError
	require.ErrorAs(t, err, &verifyErr, "the credential exchange must keep verifying the token endpoint")
	assert.Zero(t, exchanges.Load())
}
