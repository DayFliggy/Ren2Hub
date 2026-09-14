package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioStreamPreservesExplicitFalse(t *testing.T) {
	for _, test := range []struct {
		body            string
		stream, present bool
	}{
		{`{}`, false, false},
		{`{"stream":false}`, false, true},
		{`{"stream":true}`, true, true},
		{`{"stream_format":"sse"}`, true, false},
	} {
		var request AudioRequest
		require.NoError(t, kitutil.Unmarshal([]byte(test.body), &request))
		assert.Equal(t, test.stream, request.IsStream(nil))
		assert.Equal(t, test.present, request.Stream != nil)
		encoded, err := kitutil.Marshal(request)
		require.NoError(t, err)
		if test.present && !test.stream {
			assert.Contains(t, string(encoded), `"stream":false`)
		}
	}
}
