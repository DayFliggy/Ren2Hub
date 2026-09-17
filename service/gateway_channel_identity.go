package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const gatewaySelectedEndpointKey = "gateway_selected_endpoint"

// Capture the identity used to build the outbound request, not a later DB view.
func CaptureGatewayChannelIdentity(c *gin.Context, channel *model.Channel) {
	c.Set(gatewaySelectedEndpointKey, channel.GatewayEndpointID())
}

// Admission and the final send both reject stale metadata. Rebuilding only an
// Authorization header would leave the URL, conversion and state owner stale.
func ValidateGatewayChannelIdentity(c *gin.Context, channelID int) error {
	endpoint := c.GetString(gatewaySelectedEndpointKey)
	if endpoint == "" || model.DB == nil {
		return ErrAdmissionRuntime
	}
	var current model.Channel
	if err := model.DB.WithContext(c.Request.Context()).First(&current, channelID).Error; err != nil {
		return err
	}
	if current.Status != common.ChannelStatusEnabled || current.GatewayEndpointID() != endpoint {
		return ErrAdmissionRuntime
	}
	key := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if !current.HasEnabledGatewayKey(key) {
		return ErrAdmissionRuntime
	}
	return nil
}
