package billing_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
)

func TestOperationalMetricsUseBoundedLabels(t *testing.T) {
	before := billing.ReadOperationalMetrics()
	billing.RecordSourceRefreshFailure("sub2api")
	billing.RecordCandidateRejection("model-name-must-not-be-a-label")
	billing.RecordMissingPrice()
	billing.RecordCalculation(false)
	billing.RecordSettlement(billing.SettlementFrozenEstimate)
	billing.RecordSettlement("request-id-must-not-be-a-label")
	after := billing.ReadOperationalMetrics()
	assert.Equal(t, "instance", after.CounterScope)
	assert.Equal(t, before.SourceRefreshFailures["sub2api"]+1, after.SourceRefreshFailures["sub2api"])
	assert.Equal(t, before.CandidateRejections["other"]+1, after.CandidateRejections["other"])
	assert.Len(t, after.CandidateRejections, 3)
	assert.NotContains(t, after.CandidateRejections, "model-name-must-not-be-a-label")
	assert.Equal(t, before.MissingPrice+1, after.MissingPrice)
	assert.Equal(t, before.Calculations+1, after.Calculations)
	assert.Equal(t, before.CalculationFailures+1, after.CalculationFailures)
	assert.Equal(t, before.Settlements[billing.SettlementFrozenEstimate]+1, after.Settlements[billing.SettlementFrozenEstimate])
	assert.Len(t, after.Settlements, 3)
	assert.NotContains(t, after.Settlements, "request-id-must-not-be-a-label")
}

func TestSettlementBasisLabelsEvidence(t *testing.T) {
	for _, test := range []struct {
		usage *billing.Usage
		want  string
	}{
		{&billing.Usage{Source: "provider", Terminal: "completed", Complete: true}, billing.SettlementConfirmed},
		{&billing.Usage{Source: "estimated_partial", Estimated: true}, billing.SettlementEstimated},
		{&billing.Usage{Source: billing.UsageSourceRequestEstimate, Estimated: true}, billing.SettlementFrozenEstimate},
	} {
		assert.Equal(t, test.want, billing.SettlementBasis(test.usage))
	}
}
