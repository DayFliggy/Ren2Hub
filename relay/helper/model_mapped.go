package helper

import (
	"github.com/QuantumNous/new-api/pkg/modelmapping"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *common.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &common.ChannelMeta{}
	}
	if info.UpstreamModelName == "" {
		info.UpstreamModelName = info.OriginModelName
	}
	mapping, err := modelmapping.Parse(c.GetString("model_mapping"))
	if err != nil {
		return err
	}
	upstreamModel, mapped, err := modelmapping.Resolve(info.OriginModelName, mapping)
	if err != nil {
		return err
	}
	if mapped {
		info.UpstreamModelName = upstreamModel
		info.IsModelMapped = true
	}
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
