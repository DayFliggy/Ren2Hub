package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo           *relaycommon.RelayInfo
	funding             FundingSource
	recoveryID          string
	preConsumedQuota    int  // 实际预扣额度（信任用户可能为 0）
	tokenConsumed       int  // 令牌额度实际扣减量
	extraReserved       int  // 发送前补充预扣的额度（订阅退款时需要单独回滚）
	trusted             bool // 是否命中信任额度旁路
	fundingSettled      bool // funding.Settle 已成功，资金来源已提交
	settled             bool // Settle 全部完成（资金 + 令牌）
	refunded            bool // 所有退款组件均已成功
	recoveryNeedsRefund bool // 预扣回滚失败，必须由 durable recovery 接管
	fundingRefunded     bool
	extraRefunded       bool
	tokenRefunded       bool
	refundInProgress    bool
	refundErr           error
	recoveryStop        chan struct{}
	recoveryDone        chan struct{}
	recoveryStopOnce    sync.Once
	mu                  sync.Mutex
}

// Settle 根据实际消耗额度进行结算。
// 资金来源和令牌额度分两步提交：若资金来源已提交但令牌调整失败，
// 会标记 fundingSettled 防止 Refund 对已提交的资金来源执行退款。
func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	delta := actualQuota - s.preConsumedQuota
	if s.recoveryID != "" {
		if err := model.UpdateBillingRecoverySettlement(s.recoveryID, int64(delta)); err != nil {
			return err
		}
	}
	if delta == 0 {
		if s.recoveryID != "" {
			if err := model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationSettle); err != nil {
				return err
			}
			if err := model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle); err != nil {
				return err
			}
		}
		s.settled = true
		if s.recoveryID != "" {
			if err := model.MarkBillingRecoverySettled(s.recoveryID); err != nil {
				s.settled = false
				return err
			}
		}
		s.stopRecoveryHeartbeat()
		return nil
	}
	// 1) 调整资金来源（仅在尚未提交时执行，防止重复调用）
	if !s.fundingSettled {
		var err error
		if s.recoveryID != "" {
			err = model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationSettle)
		} else {
			err = s.funding.Settle(delta)
		}
		if err != nil {
			return err
		}
		s.fundingSettled = true
	}
	// 2) 调整令牌额度。资金结算成功后保留 fundingSettled=true；令牌
	// 调整失败时不标记整个会话 settled，后续结算调用可以只重试令牌部分。
	var tokenErr error
	if s.recoveryID != "" {
		tokenErr = model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle)
	} else if !s.relayInfo.IsPlayground {
		if delta > 0 {
			tokenErr = model.DecreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta)
		} else {
			tokenErr = model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, -delta)
		}
		if tokenErr != nil {
			common.SysLog(fmt.Sprintf("error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s",
				s.relayInfo.UserId, s.relayInfo.TokenId, delta, tokenErr.Error()))
			return tokenErr
		}
	}
	// 3) 更新 relayInfo 上的订阅 PostDelta（用于日志）
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	s.settled = true
	if s.recoveryID != "" {
		if err := model.MarkBillingRecoverySettled(s.recoveryID); err != nil {
			s.settled = false
			return err
		}
	}
	s.stopRecoveryHeartbeat()
	return nil
}

// Refund 退还所有预扣费。各组件独立推进，只有全部组件成功后才标记
// refunded；失败组件保持 pending，后续恢复调用可以继续重试而不会重复
// 已完成的组件。
func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	if s.settled || s.refunded || s.refundInProgress || !s.needsRefundLocked() {
		s.mu.Unlock()
		return
	}
	s.refundInProgress = true
	fundingRefunded := s.fundingRefunded
	fundingSettled := s.fundingSettled
	extraRefunded := s.extraRefunded
	tokenRefunded := s.tokenRefunded
	s.mu.Unlock()
	s.stopRecoveryHeartbeat()

	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费（token_quota=%s, funding=%s）",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
		s.funding.Source(),
	))

	tokenId := s.relayInfo.TokenId
	tokenKey := s.relayInfo.TokenKey
	isPlayground := s.relayInfo.IsPlayground
	tokenConsumed := s.tokenConsumed
	extraReserved := s.extraReserved
	subscriptionId := s.relayInfo.SubscriptionId
	funding := s.funding
	var refundErr error
	if s.recoveryID != "" {
		if err := model.MarkBillingRecoveryRefundPending(s.recoveryID, nil); err != nil {
			refundErr = fmt.Errorf("mark billing recovery pending: %w", err)
		}
	}
	if !fundingRefunded && !fundingSettled {
		var err error
		if refundErr == nil && s.recoveryID != "" {
			err = model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationRefund)
		} else if refundErr == nil {
			err = funding.Refund()
		}
		if err != nil {
			refundErr = fmt.Errorf("refund billing source: %w", err)
		} else {
			fundingRefunded = true
		}
	}
	if refundErr == nil && !extraRefunded && extraReserved > 0 && funding.Source() == BillingSourceSubscription && subscriptionId > 0 {
		var err error
		if s.recoveryID != "" {
			err = model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentExtra, model.BillingRecoveryOperationRefund)
		} else {
			err = model.PostConsumeUserSubscriptionDelta(subscriptionId, -int64(extraReserved))
		}
		if err != nil {
			refundErr = fmt.Errorf("refund subscription extra reserve: %w", err)
		} else {
			extraRefunded = true
		}
	}
	if refundErr == nil && !tokenRefunded && tokenConsumed > 0 && !isPlayground {
		var err error
		if s.recoveryID != "" {
			err = model.ApplyBillingRecoveryAdjustment(s.recoveryID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationRefund)
		} else {
			err = model.IncreaseTokenQuota(tokenId, tokenKey, tokenConsumed)
		}
		if err != nil {
			refundErr = fmt.Errorf("refund token quota: %w", err)
		} else {
			tokenRefunded = true
		}
	}

	s.mu.Lock()
	s.fundingRefunded = fundingRefunded
	s.extraRefunded = extraRefunded
	if isPlayground || tokenConsumed <= 0 {
		s.tokenRefunded = true
	} else {
		s.tokenRefunded = tokenRefunded
	}
	s.refundInProgress = false
	s.refundErr = refundErr
	s.relayInfo.BillingRefundError = refundErr
	if refundErr == nil && s.refundComponentsCompleteLocked(isPlayground, tokenConsumed) {
		s.refunded = true
	}
	s.mu.Unlock()
	if refundErr != nil {
		common.SysLog("billing refund remains pending: " + refundErr.Error())
	} else {
		s.mu.Lock()
		recoveryComplete := s.recoveryID != "" && s.refundComponentsCompleteLocked(isPlayground, tokenConsumed)
		s.mu.Unlock()
		if !recoveryComplete {
			return
		}
		if err := model.MarkBillingRecoveryRefunded(s.recoveryID, nil); err != nil {
			common.SysLog("billing recovery completion remains pending: " + err.Error())
			s.mu.Lock()
			s.refundErr = err
			s.relayInfo.BillingRefundError = err
			s.mu.Unlock()
		}
	}
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded {
		return false
	}
	if !s.fundingSettled && !s.fundingRefunded {
		if s.tokenConsumed > 0 || (func() bool {
			sub, ok := s.funding.(*SubscriptionFunding)
			return ok && sub.preConsumed > 0
		})() {
			return true
		}
	}
	if !s.extraRefunded && s.extraReserved > 0 && s.funding.Source() == BillingSourceSubscription {
		return true
	}
	if !s.tokenRefunded && s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
		return true
	}
	return false
}

func (s *BillingSession) refundComponentsCompleteLocked(isPlayground bool, tokenConsumed int) bool {
	if !s.fundingSettled && !s.fundingRefunded {
		return false
	}
	if s.extraReserved > 0 && s.funding.Source() == BillingSourceSubscription && !s.extraRefunded {
		return false
	}
	return isPlayground || tokenConsumed <= 0 || s.tokenRefunded
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled || s.refunded || s.trusted || targetQuota <= s.preConsumedQuota {
		return nil
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	if err := s.reserveFunding(delta); err != nil {
		return err
	}
	if err := s.reserveToken(delta); err != nil {
		if rollbackErr := s.rollbackFundingReserve(delta); rollbackErr != nil {
			// The token reservation did not commit, but the funding side may
			// still be charged. Keep that delta in the session so Refund and
			// durable recovery can continue from the actual state.
			s.preConsumedQuota += delta
			s.extraReserved += delta
			s.syncRelayInfo()
			s.recoveryNeedsRefund = true
			common.SysLog("billing funding rollback remains pending: " + rollbackErr.Error())
			if s.recoveryID != "" {
				if syncErr := syncBillingRecoveryWithRetry(s); syncErr != nil {
					common.SysLog("billing funding rollback recovery sync remains pending: " + syncErr.Error())
				}
			}
		}
		return err
	}

	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.extraReserved += delta
	s.syncRelayInfo()
	if s.recoveryID != "" {
		if err := syncBillingRecoveryWithRetry(s); err != nil {
			rollbackErr := s.rollbackReservedDelta(delta)
			if syncErr := syncBillingRecoveryWithRetry(s); syncErr != nil {
				common.SysLog("billing reserve rollback recovery sync remains pending: " + syncErr.Error())
			}
			if rollbackErr != nil {
				s.recoveryNeedsRefund = true
				common.SysLog("billing reserve rollback remains pending: " + rollbackErr.Error())
			}
			return errors.Join(err, rollbackErr)
		}
	}
	return nil
}

func syncBillingRecoveryWithRetry(s *BillingSession) error {
	if s == nil {
		return errors.New("billing session is unavailable")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.syncRecovery(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *BillingSession) rollbackReservedDelta(delta int) error {
	if delta <= 0 {
		return nil
	}
	var rollbackErr error
	if !s.relayInfo.IsPlayground {
		if err := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("rollback token reserve: %w", err))
		} else {
			s.tokenConsumed -= delta
		}
	}
	if err := s.rollbackFundingReserve(delta); err != nil {
		rollbackErr = errors.Join(rollbackErr, err)
	} else {
		s.preConsumedQuota -= delta
		s.extraReserved -= delta
	}
	s.syncRelayInfo()
	return rollbackErr
}

func (s *BillingSession) syncRecovery() error {
	if s == nil || s.recoveryID == "" {
		return nil
	}
	fundingAmount := int64(0)
	if wallet, ok := s.funding.(*WalletFunding); ok {
		fundingAmount = int64(wallet.consumed)
	} else if subscription, ok := s.funding.(*SubscriptionFunding); ok {
		fundingAmount = subscription.preConsumed
	}
	subscriptionID := s.relayInfo.SubscriptionId
	if subscription, ok := s.funding.(*SubscriptionFunding); ok {
		subscriptionID = subscription.subscriptionId
	}
	tokenAmount := int64(0)
	if !s.relayInfo.IsPlayground {
		tokenAmount = int64(s.tokenConsumed)
	}
	return model.UpdateBillingRecoveryAmounts(s.recoveryID, subscriptionID, fundingAmount, int64(s.extraReserved), tokenAmount)
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

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣费入口（含信任额度旁路）
// ---------------------------------------------------------------------------

// preConsume 执行预扣费：信任检查 -> 令牌预扣 -> 资金来源预扣。
// 任一步骤失败时原子回滚已完成的步骤。
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota

	// ---- 信任额度旁路 ----
	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, fmt.Sprintf("用户 %d 额度充足, 信任且不需要预扣费 (funding=%s)", s.relayInfo.UserId, s.funding.Source()))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 需要预扣费 %s (funding=%s)", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota), s.funding.Source()))
	}

	// ---- 1) 预扣令牌额度 ----
	if effectiveQuota > 0 {
		if err := PreConsumeTokenQuota(s.relayInfo, effectiveQuota); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		s.tokenConsumed = effectiveQuota
	}

	// ---- 2) 预扣资金来源 ----
	if err := s.funding.PreConsume(effectiveQuota); err != nil {
		// 预扣费失败，回滚令牌额度
		if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
			if rollbackErr := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, s.tokenConsumed); rollbackErr != nil {
				s.recoveryNeedsRefund = true
				common.SysLog(fmt.Sprintf("error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s",
					s.relayInfo.UserId, s.relayInfo.TokenId, s.tokenConsumed, err.Error(), rollbackErr.Error()))
				if syncErr := s.syncRecovery(); syncErr != nil {
					common.SysLog("error persisting token rollback recovery: " + syncErr.Error())
				}
			} else {
				s.tokenConsumed = 0
			}
		}
		// TODO: model 层应定义哨兵错误（如 ErrNoActiveSubscription），用 errors.Is 替代字符串匹配
		if errors.Is(err, ErrInsufficientWalletQuota) {
			userQuota, quotaErr := model.GetUserQuota(s.relayInfo.UserId, false)
			if quotaErr != nil {
				userQuota = 0
			}
			return types.NewErrorWithStatusCode(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") {
			return types.NewErrorWithStatusCode(fmt.Errorf("订阅额度不足或未配置订阅: %s", errMsg), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}

	s.preConsumedQuota = effectiveQuota

	// ---- 同步 RelayInfo 兼容字段 ----
	s.syncRelayInfo()

	return nil
}

func (s *BillingSession) reserveFunding(delta int) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		// 与结算补扣（SettleBilling 正差额 → WalletFunding.Settle）语义一致：
		// 全额无条件扣减，余额不足的部分记为欠费（余额可为负），不中断请求，
		// 保证日志记录的预扣额度与用户余额的实际变动始终对账一致。
		// DecreaseUserQuota 仅在数据库错误时失败。
		if err := model.DecreaseUserQuota(funding.userId, delta, false); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		funding.consumed += delta
		return nil
	case *SubscriptionFunding:
		if err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, int64(delta)); err != nil {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("订阅额度不足或未配置订阅: %s", err.Error()),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return nil
	default:
		return types.NewError(fmt.Errorf("unsupported funding source: %s", s.funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
}

func (s *BillingSession) rollbackFundingReserve(delta int) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if err := model.IncreaseUserQuota(funding.userId, delta, false); err != nil {
			return fmt.Errorf("rollback wallet funding reserve: %w", err)
		} else {
			funding.consumed -= delta
		}
	case *SubscriptionFunding:
		if err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, -int64(delta)); err != nil {
			return fmt.Errorf("rollback subscription funding reserve: %w", err)
		}
	}
	return nil
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// shouldTrust 统一信任额度检查，适用于钱包和订阅。
func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	// 异步任务（ForcePreConsume=true）必须预扣全额，不允许信任旁路
	if s.relayInfo.ForcePreConsume {
		return false
	}

	trustQuota := common.GetTrustQuota()
	if trustQuota <= 0 {
		return false
	}

	// 检查令牌是否充足
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenQuota := c.GetInt("token_quota")
		tokenTrusted = tokenQuota > trustQuota
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return s.relayInfo.UserQuota > trustQuota
	case BillingSourceSubscription:
		// 订阅不能启用信任旁路。原因：
		// 1. PreConsumeUserSubscription 要求 amount>0 来创建预扣记录并锁定订阅
		// 2. SubscriptionFunding.PreConsume 忽略参数，始终用 s.amount 预扣
		// 3. 若信任旁路将 effectiveQuota 设为 0，会导致 preConsumedQuota 与实际订阅预扣不一致
		return false
	default:
		return false
	}
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionPreConsumed = sub.preConsumed + int64(s.extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(s.extraReserved)
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if strings.TrimSpace(relayInfo.RequestId) == "" {
		relayInfo.RequestId = common.NewRequestId()
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)

	// 钱包路径需要先检查用户额度
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if userQuota-preConsumedQuota < 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo:  relayInfo,
			funding:    &WalletFunding{userId: relayInfo.UserId},
			recoveryID: relayInfo.RequestId,
		}
		if err := ensureBillingRecovery(session); err != nil {
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			if session.recoveryNeedsRefund {
				_ = model.MarkBillingRecoveryRefundPending(session.recoveryID, apiErr.Err)
			} else {
				_ = model.MarkBillingRecoveryRefunded(session.recoveryID, apiErr.Err)
			}
			return nil, apiErr
		}
		if err := session.syncRecovery(); err != nil {
			_ = model.MarkBillingRecoveryRefundPending(session.recoveryID, err)
			session.Refund(c)
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		session.startRecoveryHeartbeat(billingRecoveryContext(c))
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo:  relayInfo,
			recoveryID: relayInfo.RequestId,
			funding: &SubscriptionFunding{
				requestId: relayInfo.RequestId,
				userId:    relayInfo.UserId,
				modelName: relayInfo.BillingModelName(),
				amount:    subConsume,
			},
		}
		if err := ensureBillingRecovery(session); err != nil {
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证 SubscriptionFunding.amount、
		// preConsume 参数和 FinalPreConsumedQuota 三者一致，避免订阅多扣费。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			if session.recoveryNeedsRefund {
				_ = model.MarkBillingRecoveryRefundPending(session.recoveryID, apiErr.Err)
			} else {
				_ = model.MarkBillingRecoveryRefunded(session.recoveryID, apiErr.Err)
			}
			return nil, apiErr
		}
		if err := session.syncRecovery(); err != nil {
			_ = model.MarkBillingRecoveryRefundPending(session.recoveryID, err)
			session.Refund(c)
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		session.startRecoveryHeartbeat(billingRecoveryContext(c))
		return session, nil
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, err := tryWallet()
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				// 仅当用户的活跃订阅允许钱包回退时才回退到钱包，否则返回订阅额度不足错误
				allowOverflow, overflowErr := model.UserActiveSubscriptionsAllowWalletOverflow(relayInfo.UserId)
				if overflowErr != nil {
					return nil, types.NewError(overflowErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
				}
				if allowOverflow {
					return tryWallet()
				}
				return nil, apiErr
			}
			return nil, apiErr
		}
		return session, nil
	}
}

func ensureBillingRecovery(session *BillingSession) error {
	if session == nil || session.relayInfo == nil || session.recoveryID == "" || session.funding == nil {
		return errors.New("billing recovery request identity is unavailable")
	}
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: session.recoveryID,
		UserID:    session.relayInfo.UserId,
		TokenID:   session.relayInfo.TokenId,
		Source:    session.funding.Source(),
	})
	return err
}

func billingRecoveryContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}
