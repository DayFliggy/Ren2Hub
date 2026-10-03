package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// WebSocket state belongs to the resolved connection, including its model.
// HTTP transport ownership alone cannot prevent a new socket from replaying
// previous_response_id after a model mapping or handshake setting changes.
func BindGatewayWebSocketIdentity(c *gin.Context, identity string) error {
	value, _ := c.Get("gateway_state_output")
	state, ok := value.(*gatewayStateOutput)
	if !ok || state == nil || identity == "" {
		return errors.New("websocket state ownership is unavailable")
	}
	state.owner.TransportID = model.GatewayDigest("responses-websocket:" + identity)
	return nil
}

const gatewaySelectedEndpointKey = "gateway_selected_endpoint"
const gatewaySelectedConfigKey = "gateway_selected_request_configuration"

type gatewayRequestConfiguration struct {
	ModelMapping, ParamOverride, HeaderOverride, Setting *string
	OtherSettings                                        string
	ChannelRatio                                         string
}

func gatewayRequestConfigurationDigest(channel *model.Channel) string {
	data, _ := common.Marshal(gatewayRequestConfiguration{channel.ModelMapping, channel.ParamOverride, channel.HeaderOverride, channel.Setting, channel.OtherSettings, channel.ChannelRatio})
	return model.GatewayDigest(string(data))
}

// Capture the identity used to build the outbound request, not a later DB view.
func CaptureGatewayChannelIdentity(c *gin.Context, channel *model.Channel) {
	c.Set(gatewaySelectedEndpointKey, channel.GatewayEndpointID())
	c.Set(gatewaySelectedConfigKey, gatewayRequestConfigurationDigest(channel))
}

// Admission and the final send both reject stale metadata. Rebuilding only an
// Authorization header would leave the URL, conversion and state owner stale.
func ValidateGatewayChannelIdentity(c *gin.Context, channelID int) error {
	return validateGatewayChannelIdentity(c, channelID, false)
}

func ValidateGatewayChannelProbeIdentity(c *gin.Context, channelID int) error {
	return validateGatewayChannelIdentity(c, channelID, true)
}

func validateGatewayChannelIdentity(c *gin.Context, channelID int, probe bool) error {
	endpoint := c.GetString(gatewaySelectedEndpointKey)
	if endpoint == "" || model.DB == nil {
		return ErrAdmissionRuntime
	}
	var current model.Channel
	if err := model.DB.WithContext(c.Request.Context()).First(&current, channelID).Error; err != nil {
		return err
	}
	if err := constant.ValidateChannelTypeLifecycle(current.Type); err != nil {
		return err
	}
	if !gatewayChannelStatusAllowsSend(current.Status, probe) || current.GatewayEndpointID() != endpoint {
		return ErrAdmissionRuntime
	}
	if c.GetString(gatewaySelectedConfigKey) != gatewayRequestConfigurationDigest(&current) {
		return ErrAdmissionRuntime
	}
	key := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if !current.HasEnabledGatewayKey(key) {
		return ErrAdmissionRuntime
	}
	return nil
}
