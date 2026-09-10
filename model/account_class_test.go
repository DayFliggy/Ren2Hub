package model

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountClassInheritanceAndExplicitUnlimited(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, InitializeAccountClasses(DB))
	a := AccountClass{Code: "policy-fixture", Name: "测试分类", DefaultConcurrency: 5, DefaultRPM: 12, TopupMultiplier: 1.25}
	require.NoError(t, CreateAccountClass(ctx, a))
	t.Cleanup(func() { DB.Where("code = ?", a.Code).Delete(&AccountClass{}) })
	inherit := User{Username: "class-inherit", AffCode:"class-inherit", AccountClass: a.Code, Quota: 12345}
	unlimited := User{Username: "class-unlimited", AffCode:"class-unlimited", AccountClass: a.Code, Quota: 23456, ConcurrencyOverride: common.GetPointer(0), RPMOverride: common.GetPointer(0)}
	require.NoError(t, DB.Create(&inherit).Error)
	require.NoError(t, DB.Create(&unlimited).Error)
	t.Cleanup(func() { DB.Unscoped().Delete(&inherit); DB.Unscoped().Delete(&unlimited) })
	limits, err := ResolveUserLimits(ctx, inherit.Id)
	require.NoError(t, err)
	assert.Equal(t, 5, limits.Concurrency)
	assert.Equal(t, 12, limits.RPM)
	limits, err = ResolveUserLimits(ctx, unlimited.Id)
	require.NoError(t, err)
	assert.Zero(t, limits.Concurrency)
	assert.Zero(t, limits.RPM)
	a.PolicyVersion = 1
	a.DefaultConcurrency = 2
	a.DefaultRPM = 3
	require.NoError(t, UpdateAccountClass(ctx, a))
	require.Error(t, UpdateAccountClass(ctx, a))
	limits, err = ResolveUserLimits(ctx, inherit.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, limits.Concurrency)
	assert.Equal(t, int64(2), limits.ClassPolicyVersion)
	require.NoError(t, DB.First(&inherit, inherit.Id).Error)
	assert.Equal(t, 12345, inherit.Quota)
	multiplier, err := GetUserTopupMultiplier(inherit.Id)
	require.NoError(t, err)
	assert.Equal(t, 1.25, multiplier)
	require.ErrorContains(t, DeleteAccountClass(ctx, a.Code), "referenced")
	require.Error(t, DeleteAccountClass(ctx, DefaultAccountClass))
}

func TestAccountClassRejectsInvalidLimitsAndMoney(t *testing.T) {
	for _, multiplier := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		a := AccountClass{Code: "valid", Name: "valid", DefaultConcurrency: 5, TopupMultiplier: multiplier}
		assert.Error(t, a.Validate())
	}
	assert.Error(t, ValidateUserLimits(&User{ConcurrencyOverride: common.GetPointer(-1)}))
	assert.Error(t, ValidateUserLimits(&User{RPMOverride: common.GetPointer(-1)}))
}
