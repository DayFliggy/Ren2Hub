package controller

import (
	"github.com/QuantumNous/new-api/relay"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func CountClaudeTokens(c *gin.Context) { relay.NativeCountTokens(c) }

func RelayGeminiAction(c *gin.Context) {
	if relayconstant.NativeCountChannelType(c.Request.URL.Path) != 0 {
		relay.NativeCountTokens(c)
		return
	}
	Relay(c, types.RelayFormatGemini)
}
