package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A catalog read that fails must stop startup. Reading it as "no legacy table"
// would skip the move, start with bonus version 0 and leave the old rates behind.
func TestTopUpBonusMigrationStopsWhenTheCatalogCannotBeRead(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(previousType) })
	require.NoError(t, sqlDB.Close())

	err = migrateTopUpBonusTables(db)

	require.ErrorContains(t, err, "check table "+legacyTopUpDiscountConfigTable)
}
