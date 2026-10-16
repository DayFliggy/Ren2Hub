package channel

import (
	"errors"
	"fmt"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type rejectedCredentialAdaptor struct{ Adaptor }

func (a rejectedCredentialAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return "https://provider.example/v1/messages", nil
}
func (a rejectedCredentialAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return fmt.Errorf("setup header: %w", &types.ProviderCredentialError{Err: errors.New("credential rejected")})
}
func TestProviderCredentialFailurePrecedesUpstreamSend(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := &relaycommon.RelayInfo{}
	_, err := BuildApiRequest(rejectedCredentialAdaptor{}, c, info, nil)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, types.ErrorCodeChannelInvalidKey, apiErr.GetErrorCode())
	require.Equal(t, 401, apiErr.StatusCode)
	require.False(t, info.UpstreamSent)
	require.False(t, types.IsSkipRetryError(apiErr))
	var credentialErr *types.ProviderCredentialError
	require.ErrorAs(t, apiErr, &credentialErr)
}
