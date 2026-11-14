//go:build known_upstream_restored

package service

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

// The summary is already computed. This helper publishes the log's aggregate,
// not the quota calculation; the original correct aggregate remains the oracle.
func TestRestoredNativeCacheWriteLogTotalIncludes12h(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary textQuotaSummary
		want    int
	}{
		{"split", textQuotaSummary{CacheCreationTokens5m: 10, CacheCreationTokens1h: 20, CacheCreationTokens12h: 40}, 70},
		{"12h_only", textQuotaSummary{CacheCreationTokens12h: 40}, 40},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, cacheWriteTokensTotal(tc.summary)) })
	}
}
