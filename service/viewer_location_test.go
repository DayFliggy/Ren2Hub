package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestParseViewerLocation(t *testing.T) {
	instant := time.Date(2026, 10, 20, 23, 30, 0, 0, time.UTC)
	for raw, want := range map[string]string{"": "2026-10-20 23:30", "0": "2026-10-20 23:30", "-480": "2026-10-21 07:30", "300": "2026-10-20 18:30", "-840": "2026-10-21 13:30", "840": "2026-10-20 09:30"} {
		loc, err := ParseViewerLocation(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, instant.In(loc).Format("2006-01-02 15:04"), raw)
	}
	for _, raw := range []string{"abc", "-841", "841", "8.5", " -480"} {
		loc, err := ParseViewerLocation(raw)
		require.Nil(t, loc, raw)
		var message *common.Message
		require.True(t, errors.As(err, &message), raw)
		require.Equal(t, "Invalid timezone offset", message.Error())
	}
}
