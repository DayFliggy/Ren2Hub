package vertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexProjectRegionAndExpressURLs(t *testing.T) {
	for _, tc := range []struct {
		name, project, region, base, model, action, want string
		mode                                             int
	}{
		{"global project", "test-project", "global", "", "gemini-4-argon", "generateContent", "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/publishers/google/models/gemini-4-argon:generateContent?key=key%26value", RequestModeGemini},
		{"us stream", "test-project", "us", "", "gemini-4-argon", "streamGenerateContent?alt=sse", "https://aiplatform.us.rep.googleapis.com/v1/projects/test-project/locations/us/publishers/google/models/gemini-4-argon:streamGenerateContent?alt=sse&key=key%26value", RequestModeGemini},
		{"eu Claude", "test-project", "eu", "", "claude-opus-4-7", "rawPredict", "https://aiplatform.eu.rep.googleapis.com/v1/projects/test-project/locations/eu/publishers/anthropic/models/claude-opus-4-7:rawPredict?key=key%26value", RequestModeClaude},
		{"regional project", "test-project", "us-central1", "", "gemini-4-argon", "generateContent", "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-4-argon:generateContent?key=key%26value", RequestModeGemini},
		{"express", "", "", "", "gemini-4-argon", "generateContent", "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-4-argon:generateContent?key=key%26value", RequestModeGemini},
		{"custom endpoint", "test-project", "eu", "https://gateway.example/v1/", "gemini-4-argon", "generateContent", "https://gateway.example/v1/projects/test-project/locations/eu/publishers/google/models/gemini-4-argon:generateContent?key=key%26value", RequestModeGemini},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "key&value", ApiVersion: tc.region, ChannelBaseUrl: tc.base, ChannelSetting: dto.ChannelSettings{VertexProjectID: tc.project}, ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeAPIKey}}}
			endpoint, err := (&Adaptor{RequestMode: tc.mode}).getRequestUrl(info, tc.model, tc.action)
			require.NoError(t, err)
			assert.Equal(t, tc.want, endpoint)
		})
	}
	t.Run("service account owns project", func(t *testing.T) {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: `{"project_id":"account-project"}`, ApiVersion: "us", ChannelSetting: dto.ChannelSettings{VertexProjectID: "ignored-project"}}}
		endpoint, err := (&Adaptor{RequestMode: RequestModeOpenSource}).getRequestUrl(info, "", "")
		require.NoError(t, err)
		assert.Equal(t, "https://aiplatform.us.rep.googleapis.com/v1beta1/projects/account-project/locations/us/endpoints/openapi/chat/completions", endpoint)
	})
}

func TestVertexDeletedServiceAccountRequiresExactProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		deleted bool
	}{
		{"deleted", 400, `{"error":"invalid_grant","error_description":"Invalid grant: account not found"}`, true},
		{"signature", 400, `{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`, false},
		{"clock", 400, `{"error":"invalid_grant","error_description":"Token must be a short-lived token"}`, false},
		{"wrong status", 500, `{"error":"invalid_grant","error_description":"Invalid grant: account not found"}`, false},
		{"other code", 400, `{"error":"temporarily_unavailable","error_description":"Invalid grant: account not found"}`, false},
		{"capacity", 429, `{"error":"resource_exhausted"}`, false},
		{"malformed", 400, `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.Form.Get("grant_type"))
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			previous := googleOAuthTokenURL
			googleOAuthTokenURL = server.URL
			t.Cleanup(func() { googleOAuthTokenURL = previous })
			token, err := exchangeJwtForAccessToken(context.Background(), "fixture-jwt", "")
			require.Error(t, err)
			assert.Empty(t, token)
			var credentialError *types.ProviderCredentialError
			assert.Equal(t, tc.deleted, errors.As(err, &credentialError))
		})
	}
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := exchangeJwtForAccessToken(ctx, "fixture-jwt", "")
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestVertexSharedCapacity429DoesNotRetryOrCoolCredential(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Resource exhausted, please try again later."}}`)
	}))
	t.Cleanup(upstream.Close)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "fixture-key", ChannelBaseUrl: upstream.URL, UpstreamModelName: "gemini-4-argon", ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeAPIKey}}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	adaptor := &Adaptor{RequestMode: RequestModeGemini}
	value, err := adaptor.DoRequest(c, info, strings.NewReader(`{"contents":[]}`))
	require.NoError(t, err)
	response := value.(*http.Response)
	defer response.Body.Close()
	assert.Equal(t, 429, response.StatusCode)
	assert.Equal(t, 1, calls)
	assert.True(t, info.UpstreamSent)
	assert.False(t, info.UpstreamAccepted)
	classification := service.ClassifyRouteError(response.StatusCode, "RESOURCE_EXHAUSTED", "", "Resource exhausted, please try again later.", false)
	assert.Equal(t, service.RouteErrorTransient, classification.Class)
	assert.False(t, classification.MarkKey)
	assert.True(t, classification.Failoverable)
	assert.True(t, classification.MarkChannelModel)
}
