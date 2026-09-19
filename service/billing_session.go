package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BillingSession only coordinates the request. Persistent accounting state is
// authoritative, including for repeated calls on an already completed session.
type BillingSession struct {
	relayInfo         *relaycommon.RelayInfo
	recoveryID        string
	preConsumedQuota  int
	settlementStarted bool
	settled           bool
	refunded          bool
	refundErr         error
	recoveryStop      chan struct{}
	recoveryDone      chan struct{}
	recoveryStopOnce  sync.Once
	mu                sync.Mutex
}

func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actualQuota < 0 || actualQuota > math.MaxInt32 {
		return errors.New("invalid settlement quota")
	}
	s.settlementStarted = true
	recovery, err := loadBillingRecovery(s.recoveryID)
	if err != nil {
		return err
	}
	if err := model.UpdateBillingRecoverySettlement(s.recoveryID, int64(actualQuota)-int64(recovery.FundingAmount)); err != nil {
		return err
	}
	if err := model.Settle(s.recoveryID, common.Quota(actualQuota)); err != nil {
		return err
	}
	s.settled = true
	s.stopRecoveryHeartbeat()
	return nil
}

func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.settlementStarted || s.refunded {
		return
	}
	info := s.relayInfo
	rejected := info.UpstreamRejected
	if c != nil {
		value, _ := c.Get("gateway_last_attempt_outcome")
		outcome, _ := value.(AttemptOutcome)
		rejected = rejected || outcome.Acceptance == AttemptRejected
	}
	if info.HasValidOutput() || !info.UpstreamPreparationOnly && !rejected && (info.UpstreamAccepted || info.UpstreamSent) {
		s.stopRecoveryHeartbeat()
		logger.LogError(c, "billing requires reconciliation (request_id="+info.RequestId+"): upstream acceptance or consumption is unknown")
		return
	}
	s.stopRecoveryHeartbeat()
	err := model.MarkBillingRecoveryRefundPending(s.recoveryID, nil)
	if err == nil {
		err = model.Release(s.recoveryID)
	}
	s.refundErr = err
	info.BillingRefundError = err
	s.refunded = err == nil
	if err != nil {
		logger.LogError(c, "billing release remains pending: "+err.Error())
	}
}

func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.settled && !s.settlementStarted && !s.refunded
}
func (s *BillingSession) GetPreConsumedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preConsumedQuota
}
func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded || s.settlementStarted {
		return model.ErrBillingRecoveryStateConflict
	}
	if !s.relayInfo.PriceData.FreeModel && targetQuota == 0 {
		targetQuota = 1
	}
	if err := model.IncreaseReservation(s.recoveryID, common.Quota(targetQuota)); err != nil {
		return err
	}
	recovery, err := loadBillingRecovery(s.recoveryID)
	if err != nil {
		return err
	}
	s.preConsumedQuota = int(recovery.FundingAmount)
	s.syncRelayInfo()
	return nil
}
func (s *BillingSession) syncRelayInfo() {
	s.relayInfo.FinalPreConsumedQuota = s.preConsumedQuota
	s.relayInfo.BillingSource = BillingSourceWallet
}

func (s *BillingSession) startRecoveryHeartbeat(ctx context.Context) {
	if s == nil || s.recoveryID == "" || s.recoveryStop != nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.recoveryStop = make(chan struct{})
	s.recoveryDone = make(chan struct{})
	stop := s.recoveryStop
	done := s.recoveryDone
	requestID := s.recoveryID
	go func() {
		defer close(done)
		ticker := time.NewTicker(BillingRecoveryHeartbeatInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if err := model.TouchBillingRecovery(requestID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					common.SysLog("billing recovery heartbeat failed: " + err.Error())
				}
			}
		}
	}()
}

func (s *BillingSession) stopRecoveryHeartbeat() {
	if s == nil || s.recoveryStop == nil {
		return
	}
	s.recoveryStopOnce.Do(func() { close(s.recoveryStop) })
	if s.recoveryDone != nil {
		<-s.recoveryDone
	}
}

func NewBillingSession(c *gin.Context, info *relaycommon.RelayInfo, quota int) (*BillingSession, *types.NewAPIError) {
	if info == nil {
		return nil, types.NewError(errors.New("missing relay info"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if strings.TrimSpace(info.RequestId) == "" {
		info.RequestId = common.NewRequestId()
	}
	if !info.PriceData.FreeModel && quota == 0 {
		quota = 1
	}
	session := &BillingSession{relayInfo: info, recoveryID: info.RequestId}
	if err := ensureBillingRecovery(session); err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if err := model.Reserve(info.RequestId, common.Quota(quota)); err != nil {
		recordBillingPreConsumeFailure(session, err)
		code := types.ErrorCodeUpdateDataError
		status := http.StatusInternalServerError
		if errors.Is(err, model.ErrBillingRecoveryFundingQuota) {
			code = types.ErrorCodeInsufficientUserQuota
			status = http.StatusForbidden
		}
		if errors.Is(err, model.ErrBillingRecoveryTokenQuota) {
			code = types.ErrorCodePreConsumeTokenQuotaFailed
			status = http.StatusForbidden
		}
		return nil, types.NewErrorWithStatusCode(err, code, status, types.ErrOptionWithSkipRetry())
	}
	session.preConsumedQuota = quota
	session.syncRelayInfo()
	session.startRecoveryHeartbeat(billingRecoveryContext(c))
	return session, nil
}
func ensureBillingRecovery(session *BillingSession) error {
	if session == nil || session.relayInfo == nil || session.recoveryID == "" {
		return errors.New("missing durable billing identity")
	}
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: session.recoveryID, UserID: session.relayInfo.UserId, TokenID: session.relayInfo.TokenId, Source: BillingSourceWallet, TokenRequired: !session.relayInfo.IsPlayground && session.relayInfo.TokenId > 0})
	return err
}
func recordBillingPreConsumeFailure(session *BillingSession, cause error) {
	if err := model.AbortPreparedBillingRecovery(session.recoveryID, cause); err != nil {
		common.SysLog(fmt.Sprintf("billing reservation failure requires recovery: %v", err))
	}
}
func billingRecoveryContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}
