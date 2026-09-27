package service

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const (
	ViolationFeeCodePrefix     = "violation_fee."
	CSAMViolationMarker        = "Failed check: SAFETY_CHECK_TYPE"
	ContentViolatesUsageMarker = "Content violates usage guidelines"
)

func IsViolationFeeCode(code types.ErrorCode) bool {
	return strings.HasPrefix(string(code), ViolationFeeCodePrefix)
}

func HasCSAMViolationMarker(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), CSAMViolationMarker) || strings.Contains(err.Error(), ContentViolatesUsageMarker) {
		return true
	}
	msg := err.ToOpenAIError().Message
	return strings.Contains(msg, CSAMViolationMarker) || strings.Contains(err.Error(), ContentViolatesUsageMarker)
}

func WrapAsViolationFeeGrokCSAM(err *types.NewAPIError) *types.NewAPIError {
	if err == nil {
		return nil
	}
	oai := err.ToOpenAIError()
	oai.Type = string(types.ErrorCodeViolationFeeGrokCSAM)
	oai.Code = string(types.ErrorCodeViolationFeeGrokCSAM)
	return types.WithOpenAIError(oai, err.StatusCode, types.ErrOptionWithSkipRetry())
}

// NormalizeViolationFeeError ensures:
// - if the CSAM marker is present, error.code is set to a stable violation-fee code and skip-retry is enabled.
// - if error.code already has the violation-fee prefix, skip-retry is enabled.
//
// It must be called before retry decision logic.
func NormalizeViolationFeeError(err *types.NewAPIError) *types.NewAPIError {
	if err == nil {
		return nil
	}

	if HasCSAMViolationMarker(err) {
		return WrapAsViolationFeeGrokCSAM(err)
	}

	if IsViolationFeeCode(err.GetErrorCode()) {
		oai := err.ToOpenAIError()
		return types.WithOpenAIError(oai, err.StatusCode, types.ErrOptionWithSkipRetry())
	}

	return err
}

func shouldChargeViolationFee(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetErrorCode() == types.ErrorCodeViolationFeeGrokCSAM {
		return true
	}
	// In case some callers didn't normalize, keep a safety net.
	return HasCSAMViolationMarker(err)
}

// ChargeViolationFeeIfNeeded uses the rule frozen with the original attempt.
// Its own request and event survive a completed or released primary bill.
func ChargeViolationFeeIfNeeded(ctx *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if ctx == nil || info == nil || !shouldChargeViolationFee(apiErr) {
		return false
	}
	original := info.PriceData.Quote
	if original == nil || !original.ViolationFeeEnabled {
		return false
	}
	quote, err := billing.NewViolationQuote(original)
	if err != nil {
		logger.LogError(ctx, "invalid frozen violation fee rule")
		return false
	}
	usage := billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: "confirmed_violation", Complete: true, Terminal: "completed"}
	charge, err := billing.Calculate(quote, usage)
	if err != nil {
		logger.LogError(ctx, "invalid frozen violation charge")
		return false
	}
	if charge.FinalQuota == 0 {
		return false
	}
	requestID := info.RequestId + ":violation"
	other := map[string]any{
		"violation_fee": true, "violation_fee_code": string(types.ErrorCodeViolationFeeGrokCSAM),
		"fee_quota": charge.FinalQuota, "base_amount": original.ViolationFeeUSD,
		"channel_ratio": original.ChannelRatio, "source_quote_hash": original.Hash,
		"price_scope": quote.PriceVersion.ScopeKind, "price_channel_id": quote.PriceVersion.ChannelID,
		"price_version_id": quote.PriceVersion.ID, "price_version_hash": quote.PriceVersion.Hash,
		"quote_hash": quote.Hash, "charge_result": charge,
		"status_code": apiErr.StatusCode,
	}
	log, err := model.BuildConsumeLog(ctx, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId, ModelName: info.OriginModelName,
		TokenName: ctx.GetString("token_name"), Quota: charge.FinalQuota,
		Content: "Violation fee charged", TokenId: info.TokenId,
		UseTimeSeconds: int(time.Since(info.StartTime).Seconds()), IsStream: info.IsStream,
		Group: info.UsingGroup, Other: other,
	})
	if err == nil {
		err = model.PrepareBillingSurcharge(context.WithoutCancel(ctx.Request.Context()), model.BillingRecoveryInput{
			RequestID: requestID, UserID: info.UserId, TokenID: info.TokenId,
			TokenRequired: !info.IsPlayground && info.TokenId > 0, Source: BillingSourceWallet,
		}, info.ChannelId, quote, model.BillingConsumption{
			Usage: &usage, Charge: charge, Log: *log, LogEnabled: common.LogConsumeEnabled,
			CountUsage: false, NodeName: common.NodeName,
		})
	}
	if err == nil {
		err = model.Settle(requestID, charge.FinalQuota)
	}
	if err == nil {
		err = model.ApplyBillingRecoveryConsumption(requestID)
	}
	if err != nil {
		logger.LogError(ctx, "violation accounting remains pending: "+err.Error())
		return false
	}
	return true
}
