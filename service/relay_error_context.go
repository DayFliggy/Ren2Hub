package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// BindRelayErrorContext 在日志或渠道切换前绑定本次尝试的脱敏输入。
func BindRelayErrorContext(ctx context.Context, relayErr *types.NewAPIError) {
	if c, ok := ctx.(*gin.Context); ok && c != nil && relayErr != nil {
		relayErr.BindSensitiveContext(common.UpstreamAddresses(c), common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	}
}
