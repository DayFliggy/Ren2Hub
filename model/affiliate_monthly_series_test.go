package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestAffiliateMonthlySeriesDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			dbType := common.DatabaseTypeSQLite
			if dialect != "sqlite" {
				key := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
				if dialect == "postgres" {
					key = "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
				}
				dsn := os.Getenv(key)
				if dsn == "" {
					t.Skip(key + " is not configured; isolated database required")
				}
				driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
				if dialect == "postgres" {
					driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("affmonth_%d_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				connection.SetMaxOpenConns(1)
			}
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(dbType)
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				require.NoError(t, db.Migrator().DropTable(&AffiliateRecord{}, &User{}))
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &AffiliateRecord{}))
			verifyAffiliateMonthlySeries(t, db)
		})
	}
}

func verifyAffiliateMonthlySeries(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month()-11, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 12, 0)
	inviter := User{Username: "series-owner", AffCode: "SERIES-OWNER", AffCount: 5}
	other := User{Username: "series-other", AffCode: "SERIES-OTHER"}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&other).Error)
	for index, row := range []struct {
		at      time.Time
		owner   int
		deleted bool
	}{
		{start.Add(-time.Second), inviter.Id, true},
		{start, inviter.Id, false},
		{start.AddDate(0, 1, 0).Add(-time.Second), inviter.Id, false},
		{start.AddDate(0, 1, 0), inviter.Id, true},
		{start.AddDate(0, 11, 0), inviter.Id, false},
		{start.AddDate(0, 11, 0), other.Id, false},
		{time.Unix(0, 0), inviter.Id, false},
		{end, inviter.Id, false},
	} {
		user := User{Username: fmt.Sprintf("series-invitee-%d", index), AffCode: fmt.Sprintf("SERIES-%d", index), InviterId: row.owner, InvitedAt: row.at.Unix()}
		require.NoError(t, db.Create(&user).Error)
		if row.deleted {
			require.NoError(t, db.Delete(&user).Error)
		}
	}
	for index, row := range []struct {
		at     time.Time
		owner  int
		kind   string
		reward int
	}{
		{start, inviter.Id, "accrue", 7},
		{start.AddDate(0, 11, 0), inviter.Id, "accrue", 11},
		{start, other.Id, "accrue", 17},
		{start, inviter.Id, "transfer", 100},
		{start.Add(-time.Second), inviter.Id, "accrue", 19},
		{end, inviter.Id, "accrue", 23},
	} {
		require.NoError(t, db.Create(&AffiliateRecord{UserID: row.owner, Kind: row.kind, SourceKey: fmt.Sprintf("series-source-%d", index), RewardQuota: row.reward, CreatedAt: row.at.Unix()}).Error)
	}
	info, err := GetAffiliateInfo(inviter.Id)
	require.NoError(t, err)
	require.Len(t, info.MonthlySeries, 12)
	assert.Equal(t, 5, info.Invited)
	cumulative := 1 // The deleted binding before the chart window is retained.
	for index, point := range info.MonthlySeries {
		newCount, reward := 0, 0
		switch index {
		case 0:
			newCount, reward = 2, 7
		case 1:
			newCount = 1
		case 11:
			newCount, reward = 1, 11
		}
		cumulative += newCount
		assert.Equal(t, AffiliateMonth{Month: start.AddDate(0, index, 0).Format("2006-01"), NewCount: newCount, Cumulative: cumulative, Reward: reward, Quota: reward}, point)
	}
}
