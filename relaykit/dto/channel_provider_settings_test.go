package dto

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChannelProviderSettingsRejectInvalidModesAndProjectPaths(t *testing.T) {
	for _, settings := range []ChannelSettings{
		{}, {AwsAPIType: AwsAPITypeInvokeModel}, {AwsAPIType: AwsAPITypeMantle},
		{VertexProjectID: "project-123"}, {VertexProjectID: "1234567890"},
	} {
		require.NoError(t, settings.ValidateProviderSettings())
	}
	for _, settings := range []ChannelSettings{
		{AwsAPIType: "unexpected"}, {VertexProjectID: "project/other"}, {VertexProjectID: "project?key=x"}, {VertexProjectID: "project#fragment"}, {VertexProjectID: "project%2fother"}, {VertexProjectID: ".."},
	} {
		require.Error(t, settings.ValidateProviderSettings())
	}
}
