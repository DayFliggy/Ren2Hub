package helper

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}
	result, err := relaycommon.ResolveModelMapping(info.OriginModelName, c.GetString("model_mapping"))
	if result.Mapped {
		info.IsModelMapped = true
	}
	if err != nil {
		return err
	}
	if result.DirectSelfMapping {
		info.IsModelMapped = false
		return nil
	}
	if result.MappingConfigured && info.IsModelMapped {
		info.UpstreamModelName = result.Name
	}
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
