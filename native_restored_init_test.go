//go:build known_upstream_restored

package main

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestRestoredNativeSensitiveMigrationFailureStopsStartup(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	filename := filepath.Join(directory, "migration.db")
	t.Setenv("SQLITE_PATH", filename)
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("REDIS_CONN_STRING", "")
	t.Setenv("NODE_TYPE", "")
	*common.LogDir = directory
	db, err := gorm.Open(sqlite.Open(filename), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	require.NoError(t, db.Create(&[]model.Option{
		{Key: "SensitiveWords", Value: "synthetic-blocked-word"},
		{Key: "CheckSensitiveEnabled", Value: "true"},
		{Key: request_plugin_setting.InstancesOption, Value: "{"},
	}).Error)
	sql, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sql.Close())
	err = InitResources()
	require.NotNil(t, model.DB)
	sqlAfter, closeErr := model.DB.DB()
	require.NoError(t, closeErr)
	defer sqlAfter.Close()
	var words model.Option
	require.NoError(t, model.DB.Where("key = ?", "SensitiveWords").First(&words).Error)
	assert.Equal(t, "synthetic-blocked-word", words.Value)
	var instances model.Option
	require.NoError(t, model.DB.Where("key = ?", request_plugin_setting.InstancesOption).First(&instances).Error)
	assert.Equal(t, "{", instances.Value, "failed migration must roll back")
	require.Error(t, err, "a failed sensitive-word migration must stop startup")
}
