package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Webhook receivers get the content with its {{value}} placeholders filled in
// order, like the email, Bark and Gotify senders. Formatting the content with
// Sprintf left the placeholders, appended %!(EXTRA ...) and broke %2F in links.
func TestSendWebhookNotifyFillsValuePlaceholders(t *testing.T) {
	originalClient := httpClient
	httpClient = &http.Client{}
	fetch := system_setting.GetFetchSetting()
	originalFetch := *fetch
	originalWorker := system_setting.WorkerUrl
	fetch.EnableSSRFProtection = false
	system_setting.WorkerUrl = ""
	t.Cleanup(func() {
		httpClient = originalClient
		*fetch = originalFetch
		system_setting.WorkerUrl = originalWorker
	})

	for _, tc := range []struct {
		name    string
		content string
		values  []any
		want    string
	}{
		{
			name:    "values fill placeholders in order",
			content: "{{value}}, remaining quota {{value}}. Top-up link: <a href='{{value}}'>{{value}}</a>",
			values:  []any{"Your quota is running low", "$0.10", "https://example.invalid/a%2Fb", "https://example.invalid/a%2Fb"},
			want:    "Your quota is running low, remaining quota $0.10. Top-up link: <a href='https://example.invalid/a%2Fb'>https://example.invalid/a%2Fb</a>",
		},
		{
			name:    "content without values stays as written",
			content: "All channel tests have completed 100%",
			want:    "All channel tests have completed 100%",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan WebhookPayload, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				var payload WebhookPayload
				require.NoError(t, common.Unmarshal(body, &payload))
				received <- payload
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			require.NoError(t, SendWebhookNotify(server.URL, "", dto.NewNotify(dto.NotifyTypeQuotaExceed, "Quota", tc.content, tc.values)))
			payload := <-received
			assert.Equal(t, tc.want, payload.Content)
			assert.Equal(t, len(tc.values), len(payload.Values))
		})
	}
}
