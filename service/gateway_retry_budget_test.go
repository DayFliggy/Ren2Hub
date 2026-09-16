package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayRetryBudgetConcurrentReservationsAndShrink(t *testing.T) {
	var budget GatewayRetryBudget
	config := model.DefaultGatewayConfig()
	config.RetryBudgetBurst = 2
	budget.Configure(config)
	start := make(chan struct{})
	results := make(chan *RetryTicket, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			ticket, err := budget.Reserve()
			if err != nil {
				assert.ErrorIs(t, err, ErrGatewayRetryBudget)
			}
			results <- ticket
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var reserved []*RetryTicket
	for ticket := range results {
		if ticket != nil {
			reserved = append(reserved, ticket)
		}
	}
	require.Len(t, reserved, 2)
	assert.EqualValues(t, 6, budget.Snapshot().Rejected)
	config.Version++
	config.RetryBudgetBurst = 1
	budget.Configure(config)
	budget.FirstSend()
	assert.Zero(t, budget.Snapshot().AvailableHundredths)
	reserved[0].Finish(false)
	assert.Zero(t, budget.Snapshot().AvailableHundredths, "remaining reservation fills the smaller burst")
	reserved[1].Finish(true)
	assert.Zero(t, budget.Snapshot().Reserved)
	_, err := budget.Reserve()
	require.ErrorIs(t, err, ErrGatewayRetryBudget, "shrinking and finishing reservations must not mint credit")
}

func TestGatewayRetryBudgetBoundsActualExtraSends(t *testing.T) {
	var budget GatewayRetryBudget
	config := model.DefaultGatewayConfig()
	config.RetryBudgetBurst = 2
	budget.Configure(config)
	first, err := budget.Reserve()
	require.NoError(t, err)
	second, err := budget.Reserve()
	require.NoError(t, err)
	_, err = budget.Reserve()
	require.ErrorIs(t, err, ErrGatewayRetryBudget)
	for range 20 {
		budget.FirstSend()
	}
	assert.Zero(t, budget.Snapshot().AvailableHundredths, "reserved tickets count against storage capacity")
	first.Finish(false)
	first.Finish(false)
	assert.EqualValues(t, 100, budget.Snapshot().AvailableHundredths)
	second.Finish(true)
	second.Finish(false)
	third, err := budget.Reserve()
	require.NoError(t, err)
	third.Finish(true)
	for range 9 {
		budget.FirstSend()
	}
	_, err = budget.Reserve()
	require.ErrorIs(t, err, ErrGatewayRetryBudget)
	budget.FirstSend()
	fourth, err := budget.Reserve()
	require.NoError(t, err)
	fourth.Finish(true)
	s := budget.Snapshot()
	assert.EqualValues(t, 3, s.RetrySends)
	assert.Zero(t, s.AvailableHundredths)
	assert.Zero(t, s.Reserved)
}

func TestGatewayRetryBudgetConfigurationDoesNotRefill(t *testing.T) {
	var budget GatewayRetryBudget
	config := model.DefaultGatewayConfig()
	config.RetryBudgetBurst = 1
	budget.Configure(config)
	ticket, err := budget.Reserve()
	require.NoError(t, err)
	ticket.Finish(true)
	config.Version++
	config.RetryBudgetBurst = 10
	budget.Configure(config)
	_, err = budget.Reserve()
	require.ErrorIs(t, err, ErrGatewayRetryBudget)
	config.Version++
	config.RetryBudgetEnabled = false
	budget.Configure(config)
	budget.FirstSend()
	unlimited, err := budget.Reserve()
	require.NoError(t, err)
	unlimited.Finish(true)
	config.Version++
	config.RetryBudgetEnabled = true
	budget.Configure(config)
	assert.Zero(t, budget.Snapshot().AvailableHundredths)
}
