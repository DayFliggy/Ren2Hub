package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
)

// Runtime pricing requires a published immutable version, including variants.
func installExplicitConfigFixture(t *testing.T) {
	t.Helper()

	previousSelfUse := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = false

	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = previousSelfUse
	})
}

func TestExplicitBillingConfigRequiresPublishedVersion(t *testing.T) {
	installExplicitConfigFixture(t)

	const variant = "gemini-9.9-flash-nothinking"

	assert.False(t, HasModelBillingConfig(variant),
		"unpublished names are not billable")
	assert.False(t, HasExplicitModelBillingConfig(variant),
		"unpublished names must not count as billing configuration")
}

func TestExplicitBillingConfigHonorsManualEntries(t *testing.T) {
	installExplicitConfigFixture(t)
	pricingFixture(t, billing.PriceVersion{Model: "gpt-4o", Mode: billing.ModeFree})
	assert.True(t, HasExplicitModelBillingConfig("gpt-4o"))
	assert.False(t, HasExplicitModelBillingConfig("model-nobody-has-heard-of"))
}

func TestExplicitBillingConfigHonorsSelfUseMode(t *testing.T) {
	installExplicitConfigFixture(t)

	operation_setting.SelfUseModeEnabled = true
	assert.False(t, HasExplicitModelBillingConfig("model-nobody-has-heard-of"), "self-use is not a published free price")
}
