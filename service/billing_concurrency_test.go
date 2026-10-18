package service

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestForcedPreconsumeRetryReserveAndRefundBalances(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 1_000_000)
	seedToken(t, 901, 901, "reserve-901", 1_000_000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("token_quota", 1_000_000)
	info := &relaycommon.RelayInfo{UserId: 901, TokenId: 901, TokenKey: "reserve-901", TokenUnlimited: true}
	session, apiErr := NewBillingSession(c, info, 100)
	require.Nil(t, apiErr)
	require.Equal(t, 100, session.GetPreConsumedQuota(), "ample wallet balance must still preconsume")
	require.NoError(t, session.Reserve(300))
	require.NoError(t, session.Reserve(200))
	require.NoError(t, session.Reserve(300))
	quota, err := model.GetUserQuota(901, false)
	require.NoError(t, err)
	require.Equal(t, 999700, quota)
	require.Equal(t, 300, session.funding.(*WalletFunding).consumed, "retry must accumulate the refundable reservation")
	session.Refund(c)
	session.Refund(c)
	require.Eventually(t, func() bool { quota, err := model.GetUserQuota(901, false); return err == nil && quota == 1_000_000 }, time.Second, time.Millisecond)
	token, err := model.GetTokenById(901)
	require.NoError(t, err)
	require.Equal(t, 1_000_000, token.RemainQuota)
}
