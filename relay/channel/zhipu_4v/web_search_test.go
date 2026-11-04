package zhipu_4v

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZhipuSearchUsageCountsCallsNotResults(t *testing.T) {
	for _, raw := range []string{
		`{"web_search":[{"title":"a"}]}`,
		`{"web_search":[{"title":"a"},{"title":"b"}],"usage":{"total_tokens":100}}`,
	} {
		require.Equal(t, map[string]int{"search_std": 1}, zhipuChatToolUsage([]byte(raw)))
	}
	for _, raw := range []string{
		`{}`, `{"web_search":[]}`, `{"web_search":null}`, `{"web_search":{}}`,
		`{"web_search":"result"}`, `{"response":{"web_search":[{}]}}`,
	} {
		require.Nil(t, zhipuChatToolUsage([]byte(raw)), raw)
	}
}
