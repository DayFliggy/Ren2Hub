package model

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFlushTest(t *testing.T) {
	t.Helper()
	savedDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&FlushOperation{}, &User{}, &Token{}, &Channel{}, &QuotaData{}))
	DB = db
	pendingQuotaFlush = nil
	pendingQuotaData = nil
	pendingQuotaDataID = ""
	CacheQuotaData = make(map[string]*QuotaData)
	for i := range batchUpdateStores {
		batchUpdateStores[i] = make(map[int]int)
	}
	t.Cleanup(func() {
		DB = savedDB
		pendingQuotaFlush = nil
		pendingQuotaData = nil
		pendingQuotaDataID = ""
		CacheQuotaData = make(map[string]*QuotaData)
		for i := range batchUpdateStores {
			batchUpdateStores[i] = make(map[int]int)
		}
		require.NoError(t, sqlDB.Close())
	})
}

func TestFlushBatchRollbackKeepsDeltasAndRetryAppliesOnce(t *testing.T) {
	setupFlushTest(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "flush", Quota: 100}).Error)
	require.NoError(t, DB.Create(&Token{Id: 1, Key: "fixture-token", RemainQuota: 100}).Error)
	addNewRecord(BatchUpdateTypeUserQuota, 1, -10)
	addNewRecord(BatchUpdateTypeTokenQuota, 1, -10)
	addNewRecord(BatchUpdateTypeChannelUsedQuota, 7, 10)
	require.Error(t, FlushBatchUpdates(context.Background()))
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 100, user.Quota)
	require.NotNil(t, pendingQuotaFlush)
	require.NoError(t, DB.Create(&Channel{Id: 7, Key: "fixture"}).Error)
	addNewRecord(BatchUpdateTypeUserQuota, 1, -5)
	require.NoError(t, FlushBatchUpdates(context.Background()))
	require.NoError(t, FlushBatchUpdates(context.Background()))
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 85, user.Quota)
	var token Token
	require.NoError(t, DB.First(&token, 1).Error)
	assert.Equal(t, 90, token.RemainQuota)
	assert.Equal(t, 10, token.UsedQuota)
	var channel Channel
	require.NoError(t, DB.First(&channel, 7).Error)
	assert.Equal(t, int64(10), channel.UsedQuota)
}

func TestFlushReceiptHandlesLostCommitAcknowledgement(t *testing.T) {
	setupFlushTest(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "flush", Quota: 100}).Error)
	id := "frozen-operation"
	apply := func(tx *gorm.DB) error {
		return tx.Model(&User{}).Where("id = ?", 1).Update("quota", gorm.Expr("quota - ?", 10)).Error
	}
	require.NoError(t, ApplyFlushOperation(context.Background(), id, apply))
	// The caller did not observe the successful commit and retries the same ID.
	require.NoError(t, ApplyFlushOperation(context.Background(), id, apply))
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 90, user.Quota)
	require.Error(t, ApplyFlushOperation(context.Background(), "failed", func(tx *gorm.DB) error {
		require.NoError(t, apply(tx))
		return errors.New("later write failed")
	}))
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 90, user.Quota)
	var count int64
	require.NoError(t, DB.Model(&FlushOperation{}).Where("id = ?", "failed").Count(&count).Error)
	assert.Zero(t, count)
}

func TestQuotaDataFailedFlushKeepsFrozenBatchAndNewArrivals(t *testing.T) {
	setupFlushTest(t)
	params := QuotaDataLogParams{UserID: 1, Username: "fixture", ModelName: "model", CreatedAt: 3600, Quota: 12, TokenUsed: 5}
	LogQuotaData(params)
	injected := errors.New("injected write failure")
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("fail_quota_fixture", func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_data" {
			tx.AddError(injected)
		}
	}))
	require.ErrorIs(t, SaveQuotaDataCache(), injected)
	require.NotNil(t, pendingQuotaData)
	LogQuotaData(params)
	require.NoError(t, DB.Callback().Create().Remove("fail_quota_fixture"))
	require.NoError(t, SaveQuotaDataCache())
	require.NoError(t, SaveQuotaDataCache())
	var rows []QuotaData
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].Count)
	assert.Equal(t, 24, rows[0].Quota)
	assert.Equal(t, 10, rows[0].TokenUsed)
}
