package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 消费日志里的渠道倍率必须是计费实际使用的精确十进制，不能按两位小数截断。
func TestConsumeLogWritesExactChannelRatio(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{}}

	for ratio, want := range map[float64]string{
		0.125:     "0.125",
		1.005:     "1.005",
		1000:      "1000",
		1e-18:     "0.000000000000000001",
		2.0000001: "2.0000001",
	} {
		text := GenerateTextOtherInfo(ctx, info, 1, ratio, 1, 0, 0, 0).Snapshot()
		assert.Equal(t, want, text["channel_ratio"])
		mj := GenerateMjOtherInfo(info, hosttypes.PriceData{ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: ratio}}).Snapshot()
		assert.Equal(t, want, mj["channel_ratio"])
	}
}
