//go:build known_upstream_restored

package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"testing"
)

func round3ControlDB(t *testing.T) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector = sqlite.Open(filepath.Join(t.TempDir(), "probe.db"))
	kind := common.DatabaseTypeSQLite
	switch os.Getenv("TEST_CONTROL_DIALECT") {
	case "", "sqlite":
	case "mysql":
		require.NotEmpty(t, os.Getenv("TEST_MYSQL_DSN"))
		driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
		kind = common.DatabaseTypeMySQL
	case "postgres":
		require.NotEmpty(t, os.Getenv("TEST_POSTGRES_DSN"))
		driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
		kind = common.DatabaseTypePostgreSQL
	default:
		t.Fatal("unexpected controlled database dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	require.NoError(t, err)
	if kind != common.DatabaseTypeSQLite {
		require.NoError(t, db.Migrator().DropTable(&Ability{}, &Channel{}))
	}
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	old, oldMemory, oldType := DB, common.MemoryCacheEnabled, common.MainDatabaseType()
	DB = db
	common.MemoryCacheEnabled = false
	common.SetMainDatabaseType(kind)
	t.Cleanup(func() {
		DB = old
		common.MemoryCacheEnabled = oldMemory
		common.SetMainDatabaseType(oldType)
		sqlDB, _ := db.DB()
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestRestoredRound3ProviderCompactFilterAttribution(t *testing.T) {
	const key = "round3-provider-compact"
	_, err := jsplugin.DefaultRegistry.Register(`export const meta = {apiVersion: 1, key: "round3-provider-compact", name: "Compact", version: "1.0.0", author: {name: "Test"}, relay: [{incoming_path: "/v1/responses/compact", upstream_path: "/compact", models: ["supported"]}]};`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	channel := &Channel{Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled}
	channel.SetSetting(kitdto.ChannelSettings{TaskPluginKey: key})
	filters := []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: "/v1/responses/compact"}, {Kind: dto.FilterTaskPluginIdentity, RequestPath: "/v1/responses/compact"}}
	ok, _ := ChannelSatisfiesFilters(channel, "supported", filters)
	require.True(t, ok)
	ok, kind := ChannelSatisfiesFilters(channel, "other", filters)
	require.False(t, ok)
	assert.Equal(t, dto.FilterRequestPath, kind)
	channel.SetSetting(kitdto.ChannelSettings{TaskPluginKey: "uninstalled"})
	ok, _ = ChannelSatisfiesFilters(channel, "supported", filters)
	require.False(t, ok)
}

func TestRestoredRound3AbilityModelNormalization(t *testing.T) {
	rows := []struct {
		name, models string
		expected     []string
	}{
		{"empty", "", []string{}}, {"commas", ",", []string{}}, {"whitespace_duplicates", " beta,alpha,beta ", []string{"alpha", "beta"}}, {"middle_empty", "m,,m", []string{"m"}}, {"same_name", "m,m", []string{"m"}},
	}
	for _, method := range []string{"add", "update"} {
		for _, row := range rows {
			t.Run(method+"/"+row.name, func(t *testing.T) {
				db := round3ControlDB(t)
				ch := Channel{Id: 715, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "fixture", Key: "fixture", Models: row.models}
				require.NoError(t, db.Create(&ch).Error)
				if method == "add" {
					require.NoError(t, ch.AddAbilities(db))
				} else {
					require.NoError(t, db.Create(&Ability{ChannelId: ch.Id, Model: "old", Enabled: true}).Error)
					require.NoError(t, ch.UpdateAbilities(nil))
				}
				var names []string
				require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", ch.Id).Order("model").Pluck("model", &names).Error)
				assert.ElementsMatch(t, row.expected, names, "ability names were normalized and empty names removed")
			})
		}
	}
}

func TestRestoredRound3PublicAbilityZeroWeight(t *testing.T) {
	db := round3ControlDB(t)
	tag := "zero-weight"
	weight := uint(0)
	require.NoError(t, db.Create(&Ability{ChannelId: 716, Model: "m", Enabled: true, Tag: &tag, Weight: 9}).Error)
	require.NoError(t, UpdateAbilityByTag(tag, nil, nil, &weight))
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", 716).First(&ability).Error)
	assert.Zero(t, ability.Weight, "the public ability update historically wrote zero weight")
}
