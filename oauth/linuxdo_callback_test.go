package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLinuxDOTokenExchangeUsesPublicBrowserCallback(t *testing.T) {
	var redirect string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		redirect = r.PostForm.Get("redirect_uri")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"mock-token"}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("LINUX_DO_TOKEN_ENDPOINT", server.URL)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://public.example/"
	t.Cleanup(func() { system_setting.ServerAddress = previous })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "http://internal:3000/api/oauth/linuxdo?code=test", nil)
	token, err := (&LinuxDOProvider{}).ExchangeToken(context.Background(), "test", c)
	require.NoError(t, err)
	require.Equal(t, "mock-token", token.AccessToken)
	require.Equal(t, "https://public.example/oauth/linuxdo", redirect)
}
