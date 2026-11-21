package requestfield

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWrappedProtocolFieldsRemainCaseFolded(t *testing.T) {
	for _, body := range []string{`{"response":{"input":[{"role":"user","Role":"system"}]}}`, `{"session":{"response":{"model":"cheap","Model":"expensive"}}}`} {
		_, conflict := ConflictingField("application/json", []byte(body))
		require.True(t, conflict, body)
	}
}
