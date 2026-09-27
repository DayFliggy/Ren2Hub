package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func acceptedMidjourneyBillingFixture(t *testing.T, price string) *model.Midjourney {
	t.Helper()
	truncate(t)
	seedUser(t, 1, 100)
	seedToken(t, 1, 1, "mj-quota", 100)
	seedChannel(t, 1)
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "mj_imagine", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: price}}}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	task := &model.Midjourney{UserId: 1, ChannelId: 1, BillingChannelId: 1, TokenId: 1, MjId: "mj-final", Action: "IMAGINE", Status: "SUBMITTED", Quota: 20,
		PrivateData: model.TaskPrivateData{TokenId: 1, BillingSource: "wallet", SubmissionRequestID: "mj-submit-final", CredentialVersion: 1, CredentialID: "credential", EndpointID: "endpoint",
			BillingContext: &model.TaskBillingContext{Quote: quote, Usage: &billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: "accepted_parameters", Complete: true}}}}
	_, err = model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: task.PrivateData.SubmissionRequestID, UserID: 1, TokenID: 1, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(task.PrivateData.SubmissionRequestID, task.Quota))
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), task.PrivateData.SubmissionRequestID, task.PrivateData.SubmissionRequestID+":attempt", task.ChannelId, quote))
	require.NoError(t, model.PersistAcceptedMidjourney(task, task.PrivateData.SubmissionRequestID, model.BillingConsumption{Log: model.Log{UserId: 1, ChannelId: 1, TokenId: 1, Quota: task.Quota}, CountUsage: true, LogEnabled: true}))
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

func TestMidjourneyMissingQuoteRetainsReservationForReconciliation(t *testing.T) {
	task := acceptedMidjourneyBillingFixture(t, "0.000006")
	task.PrivateData.BillingContext.Quote = nil
	task.Status = "SUCCESS"
	won, err := CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.ErrorContains(t, err, "frozen billing quote")
	assert.False(t, won)
	assert.Equal(t, 80, getUserQuota(t, 1))
	assert.Zero(t, getTokenUsedQuota(t, 1))
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusReconciliation, recovery.Status)
	assert.Empty(t, recovery.ChargeSnapshot)
	var stored model.Midjourney
	require.NoError(t, model.DB.First(&stored, task.Id).Error)
	assert.Equal(t, "SUCCESS", stored.Status, "confirmed provider completion must stop polling even when accounting is pending")
	require.NotNil(t, stored.PrivateData.BillingContext.Quote, "an incomplete poller cannot erase the accepted quote")
}

func TestMidjourneyManualReconciliationKeepsTaskAndLogAmountsConsistent(t *testing.T) {
	task := acceptedMidjourneyBillingFixture(t, "0.000006")
	task.PrivateData.BillingContext.Quote = nil
	task.Status = "SUCCESS"
	_, err := CompleteMidjourneyBilling(context.Background(), task, "SUBMITTED")
	require.Error(t, err)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	command := model.BillingResolution{OperationID: "mj-manual", ExpectedVersion: recovery.Version, Action: "settle", Quota: 7, Reason: "provider invoice confirmed"}
	for range 2 {
		_, err = model.ResolveBillingReconciliation(context.Background(), recovery.ID, 1, command)
		require.NoError(t, err)
		require.NoError(t, model.ApplyBillingRecoveryConsumption(recovery.RequestID))
	}
	var stored model.Midjourney
	require.NoError(t, model.DB.First(&stored, task.Id).Error)
	assert.EqualValues(t, 7, stored.Quota)
	assert.Equal(t, 93, getUserQuota(t, 1))
	assert.Equal(t, 7, getTokenUsedQuota(t, 1))
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 1, user.RequestCount)
	var logs []model.Log
	require.NoError(t, model.DB.Where("request_id = ?", recovery.RequestID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.EqualValues(t, 7, logs[0].Quota)
	assert.Equal(t, 1, logs[0].ChannelId)
}
