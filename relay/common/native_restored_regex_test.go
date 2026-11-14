//go:build known_upstream_restored

package common

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const restoredRegexCacheLimit = 256
const restoredRegexCacheMaxBytes = 256 * 1024
const restoredRegexCacheMaxPatternBytes = 4096

func restoredRegexCacheState() (map[string]bool, int) {
	values := make(map[string]bool)
	bytes := 0
	paramOverrideRegexCache.Range(func(key, _ any) bool {
		pattern := key.(string)
		values[pattern] = true
		bytes += len(pattern)
		return true
	})
	return values, bytes
}

func TestRestoredProbeRegexCacheBound(t *testing.T) {
	first := t.Name() + "-0"
	for i := 0; i <= restoredRegexCacheLimit; i++ {
		_, err := getParamOverrideRegex(t.Name() + "-" + fmt.Sprint(i))
		require.NoError(t, err)
	}
	values, bytes := restoredRegexCacheState()
	assert.LessOrEqual(t, len(values), restoredRegexCacheLimit)
	assert.LessOrEqual(t, bytes, restoredRegexCacheMaxBytes)
	_, present := values[first]
	assert.False(t, present, "restored_regex_cache: old_entries_must_be_evicted")
}

func TestRestoredProbeRegexCacheOversize(t *testing.T) {
	pattern := "^" + strings.Repeat("a", restoredRegexCacheMaxPatternBytes) + "$"
	_, err := getParamOverrideRegex(pattern)
	require.NoError(t, err)
	values, _ := restoredRegexCacheState()
	_, present := values[pattern]
	assert.False(t, present, "restored_regex_cache: oversized_pattern_must_not_be_cached")
}

func TestRestoredProbeRegexCacheReleasesOldEntries(t *testing.T) {
	pattern := t.Name() + "-first"
	first, err := getParamOverrideRegex(pattern)
	require.NoError(t, err)
	for i := 0; i < 257; i++ {
		_, err := getParamOverrideRegex(fmt.Sprintf("%s-%d", t.Name(), i))
		require.NoError(t, err)
	}
	again, err := getParamOverrideRegex(pattern)
	require.NoError(t, err)
	require.True(t, first != again, "restored_regex_cache: old_administrator_patterns_must_be_released")
}
