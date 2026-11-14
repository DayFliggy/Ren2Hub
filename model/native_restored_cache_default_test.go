//go:build known_upstream_restored

package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The original conversion-matrix case left CreateCacheRatio unset. Preserve
// its exact historical expression assertion when the native fallback returns.
func TestRestoredNativeCacheWriteDefaultConversion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pricing.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Model{}, &Ability{}))
	previous, quota := DB, common.QuotaPerUnit
	DB, common.QuotaPerUnit = db, 500000
	t.Cleanup(func() {
		DB, common.QuotaPerUnit = previous, quota
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	preview, err := PreviewModelPricingConversion("vendor/claude-sonnet-4", PricingValues{"ModelRatio": float64(2), "CompletionRatio": float64(0)})
	require.NoError(t, err)
	assert.Equal(t, `tier("base", p * 4 + c * 0 + cr * 4 + cc * 4 + cc1h * 6.4)`, preview.Expression)
}
