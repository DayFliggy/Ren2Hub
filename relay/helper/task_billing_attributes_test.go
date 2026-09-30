package helper

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeTaskBillingUsesFinalProtocolParameters(t *testing.T) {
	for _, body := range []string{
		`{"duration":6,"quantity":2,"resolution":"720p","prompt":"private"}`,
		`{"duration":99,"parameters":{"durationSeconds":6,"sampleCount":2,"resolution":"720p"},"instances":[{"prompt":"private"}]}`,
		`{"parameters":{"duration":6,"n":2,"size":"720p"},"input":{"prompt":"private"}}`,
	} {
		info := &relaycommon.RelayInfo{}
		require.NoError(t, FreezeTaskBillingAttributes(info, "application/json", []byte(body)))
		assert.Equal(t, map[string]interface{}{"duration": json.Number("6"), "quantity": json.Number("2"), "resolution": "720p"}, info.BillingRequestInput.Attributes)
		encoded, err := common.Marshal(info.BillingRequestInput)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "private")
		assert.False(t, info.BillingRequestInput.FrozenAt.IsZero())
	}
}

func TestFreezeTaskBillingMultipartRejectsDuplicateAndLongAttributes(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("seconds", "6"))
		require.NoError(t, writer.WriteField("prompt", "private"))
		if duplicate {
			require.NoError(t, writer.WriteField("seconds", "8"))
		}
		require.NoError(t, writer.Close())
		info := &relaycommon.RelayInfo{}
		err := FreezeTaskBillingAttributes(info, writer.FormDataContentType(), body.Bytes())
		if duplicate {
			require.ErrorContains(t, err, "duplicate")
		} else {
			require.NoError(t, err)
			assert.Equal(t, map[string]interface{}{"duration": json.Number("6")}, info.BillingRequestInput.Attributes)
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("quality", string(bytes.Repeat([]byte("a"), 129))))
	require.NoError(t, writer.Close())
	require.Error(t, FreezeTaskBillingAttributes(&relaycommon.RelayInfo{}, writer.FormDataContentType(), body.Bytes()))
}

func TestFreezeTaskBillingRemixInheritsOnlyOmittedMediaFacts(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	inherit := map[string]interface{}{"duration": json.Number("6"), "resolution": "720p", "quantity": json.Number("2"), "quality": "high", "service_tier": "priority", "secret": "private"}
	require.NoError(t, FreezeTaskBillingAttributes(info, "application/json", []byte(`{"duration":8,"prompt":"new private"}`), inherit))
	assert.Equal(t, map[string]interface{}{"duration": json.Number("8"), "resolution": "720p", "quantity": json.Number("2"), "quality": "high"}, info.BillingRequestInput.Attributes)
	assert.Equal(t, json.Number("6"), inherit["duration"])
	require.Error(t, FreezeTaskBillingAttributes(info, "application/json", []byte(`{}`), map[string]interface{}{"duration": -1}))
}
