package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func acceptedMidjourneyBillingFixture(t *testing.T, price string) *model.Midjourney {
	t.Helper()
	truncate(t)
	seedUser(t, 1, 100)
	seedToken(t, 1, 1, "mj-quota", 100)
	seedChannel(t, 1)
	quote, err := billing.NewQuote(billingTestStoredPrice(t, "mj_imagine", billing.PricingValues{"ModelPrice": price}), "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	task := &model.Midjourney{UserId: 1, ChannelId: 1, BillingChannelId: 1, TokenId: 1, MjId: "mj-final", Action: "IMAGINE", Status: "SUBMITTED", Quota: 20,
		PrivateData: model.TaskPrivateData{TokenId: 1, BillingSource: "wallet", SubmissionRequestID: "mj-submit-final", CredentialVersion: 1, CredentialID: "credential", EndpointID: "endpoint",
			BillingContext: &model.TaskBillingContext{Quote: quote, Usage: &billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: billing.UsageSourceRequestEstimate, Estimated: true}}}}
	_, err = model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: task.PrivateData.SubmissionRequestID, UserID: 1, TokenID: 1, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(task.PrivateData.SubmissionRequestID, task.Quota))
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), task.PrivateData.SubmissionRequestID, task.PrivateData.SubmissionRequestID+":attempt", task.ChannelId, quote))
	task.PrivateData.BillingContext.AttemptID = task.PrivateData.SubmissionRequestID + ":attempt"
	estimate, charge, err := model.EvaluateBilling(quote, *task.PrivateData.BillingContext.Usage, billingexpr.RequestInput{}, task.PrivateData.SubmissionRequestID, task.PrivateData.BillingContext.AttemptID, "estimate")
	require.NoError(t, err)
	require.NoError(t, model.PersistBillingEstimate(task.PrivateData.BillingContext.AttemptID, estimate, charge))
	task.PrivateData.BillingContext.Usage = &estimate
	require.NoError(t, model.PersistAcceptedMidjourney(task, task.PrivateData.SubmissionRequestID, model.BillingConsumption{AttemptID: task.PrivateData.BillingContext.AttemptID, Log: model.Log{UserId: 1, ChannelId: 1, TokenId: 1, Quota: task.Quota}, CountUsage: true, LogEnabled: true}))
	return task
}

func TestMidjourneyFinalChargeUsesFrozenQuoteAndPersistsSeparateEvidence(t *testing.T) {
	task := acceptedMidjourneyBillingFixture(t, "0.000006")
	task.Status = "SUCCESS"
	won, err := CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.NoError(t, err)
	assert.True(t, won)
	assert.EqualValues(t, 3, task.Quota)
	assert.Equal(t, 97, getUserQuota(t, 1))
	assert.Equal(t, 97, getTokenRemainQuota(t, 1))
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	var charge billing.ChargeResult
	require.NoError(t, common.UnmarshalJsonStr(recovery.ChargeSnapshot, &charge))
	assert.EqualValues(t, 3, charge.FinalQuota)
	assert.NotEmpty(t, recovery.UsageSnapshot)
	require.NoError(t, model.DB.First(task, task.Id).Error)
	require.NotNil(t, task.PrivateData.BillingContext.Charge)
	assert.EqualValues(t, 3, task.PrivateData.BillingContext.Charge.FinalQuota)
	won, err = CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.NoError(t, err)
	assert.False(t, won)
	assert.Equal(t, 97, getUserQuota(t, 1))
	assert.EqualValues(t, 1, countLogs(t))
}

func TestMidjourneyExplicitFailureReleasesReservation(t *testing.T) {
	task := acceptedMidjourneyBillingFixture(t, "0.000006")
	task.Status, task.FailReason = "FAILURE", "provider rejected generation"
	won, err := CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.NoError(t, err)
	assert.True(t, won)
	assert.Zero(t, task.Quota)
	assert.Equal(t, 100, getUserQuota(t, 1))
	assert.Zero(t, getTokenUsedQuota(t, 1))
}

func TestMidjourneyUnpricedSuccessSettlesAtSubmissionEstimate(t *testing.T) {
	task := acceptedMidjourneyBillingFixture(t, "0.000006")
	task.PrivateData.BillingContext.Quote = nil
	task.Status = "SUCCESS"
	won, err := CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.NoError(t, err)
	assert.True(t, won)
	assert.EqualValues(t, 3, task.Quota)
	assert.Equal(t, 97, getUserQuota(t, 1))
	assert.Equal(t, 3, getTokenUsedQuota(t, 1))
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
	var settled billing.Usage
	require.NoError(t, common.UnmarshalJsonStr(string(recovery.UsageSnapshot), &settled))
	assert.Equal(t, billing.UsageSourceRequestEstimate, settled.Source)
	var stored model.Midjourney
	require.NoError(t, model.DB.First(&stored, task.Id).Error)
	assert.Equal(t, "SUCCESS", stored.Status)
	assert.EqualValues(t, 3, stored.Quota)
	require.NotNil(t, stored.PrivateData.BillingContext.Quote, "an incomplete poller cannot erase the accepted quote")
}

func TestMidjourneySettlementFailureRecoversWithConsistentTaskAndLogAmounts(t *testing.T) {
	task := acceptedMidjourneyBillingFixture(t, "0.000006")
	callback := "test:mj_settle_failure"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*model.BillingEvent); ok && event.Operation == "settle" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove(callback) })
	task.Status = "SUCCESS"
	won, err := CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.ErrorIs(t, err, assert.AnError)
	require.True(t, won, "provider completion and final evidence survive settlement failure")
	require.NoError(t, model.DB.Callback().Create().Remove(callback))
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	require.NotEmpty(t, recovery.ChargeSnapshot)
	assert.Equal(t, 80, getUserQuota(t, 1))
	// Recovery settles the stored three-quota intent once, without an operator.
	for range 2 {
		status, err := recoverOneBillingRecovery(context.Background(), recovery.RequestID)
		require.NoError(t, err)
		assert.Equal(t, model.BillingRecoveryStatusSettled, status)
	}
	var stored model.Midjourney
	require.NoError(t, model.DB.First(&stored, task.Id).Error)
	assert.EqualValues(t, 3, stored.Quota)
	assert.Equal(t, 97, getUserQuota(t, 1))
	assert.Equal(t, 3, getTokenUsedQuota(t, 1))
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 1, user.RequestCount)
	var logs []model.Log
	require.NoError(t, model.DB.Where("request_id = ?", recovery.RequestID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.EqualValues(t, 3, logs[0].Quota)
	assert.Equal(t, 1, logs[0].ChannelId)
}
