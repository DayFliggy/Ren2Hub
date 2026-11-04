package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestValidateVertexAPIVersion(t *testing.T) {
	for _, version := range []string{"", "v1", "v1beta1", "v2", "../v1", "v1?key=bad"} {
		t.Run(version, func(t *testing.T) {
			channel := &model.Channel{Type: constant.ChannelTypeVertexAi, Other: `{"default":"global"}`, OtherSettings: `{"vertex_api_version":"` + version + `"}`}
			err := validateChannel(channel, false)
			if version == "" || version == "v1" || version == "v1beta1" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "Vertex AI API version")
			}
		})
	}
}
