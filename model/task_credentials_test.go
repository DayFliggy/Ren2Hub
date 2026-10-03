package model

import (
	"context"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTaskPersistsCredentialReferenceWithoutPlaintextKey(t *testing.T) {
	task := InitTask(constant.TaskPlatform("test"), &relaycommon.RelayInfo{
		UserId: 1,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:            9,
			ChannelType:          constant.ChannelTypeGemini,
			ChannelIsMultiKey:    true,
			ChannelMultiKeyIndex: 1,
			ApiKey:               "plaintext-secret",
		},
	})

	assert.Empty(t, task.PrivateData.Key)
	assert.Equal(t, 1, task.PrivateData.ChannelKeyIndex)
	assert.Equal(t, 1, task.PrivateData.CredentialVersion)
	assert.Empty(t, ResolveTaskChannelKey(&Channel{Id: 9, Key: "first\nsecond"}, task), "an unbound new task must never use a legacy key fallback")
}

func TestTaskCredentialBindingSurvivesKeyReorderAndTrustedRefresh(t *testing.T) {
	for _, test := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " must target an isolated database service")
			}
			open := gatewayStartupDatabase(t, test.kind, dsn)
			db, err := open()
			require.NoError(t, err)
			previousDB, previousKind := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(test.kind)
			initCol()
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousKind)
				initCol()
				sqlDB, closeErr := db.DB()
				if assert.NoError(t, closeErr) {
					assert.NoError(t, sqlDB.Close())
				}
			})
			require.NoError(t, db.AutoMigrate(&Task{}, &Channel{}, &Ability{}, &GatewayCredential{}))
			ctx := context.Background()
			channel := &Channel{Key: "first\nsecond", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI,
				ChannelInfo: ChannelInfo{IsMultiKey: true}}
			require.NoError(t, db.Create(channel).Error)
			task := InitTask(constant.TaskPlatform("test"), &relaycommon.RelayInfo{UserId: 1, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}})
			task.Properties = Properties{Input: "test input", OriginModelName: "test-model", UpstreamModelName: "provider-model"}
			require.NoError(t, task.PrivateData.BindCredential(ctx, channel, "second"))
			task.PrivateData.UpstreamTaskID = "provider-task"
			task.SetData(map[string]any{"id": "provider-task", "status": "queued"})
			require.NoError(t, task.Insert())
			var stored Task
			require.NoError(t, db.First(&stored, task.ID).Error)
			assert.Equal(t, task.PrivateData, stored.PrivateData)
			assert.Equal(t, task.Properties, stored.Properties)
			assert.JSONEq(t, string(task.Data), string(stored.Data))
			assert.Empty(t, stored.PrivateData.Key)
			encoded, err := common.Marshal(stored.PrivateData)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "second")
			assert.NotContains(t, string(encoded), "first")

			channel.Key = "second\nfirst"
			require.NoError(t, channel.Update())
			key, index, err := ResolveTaskCredential(ctx, channel, &stored)
			require.NoError(t, err)
			assert.Equal(t, "second", key)
			assert.Zero(t, index)

			channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled}
			_, _, err = ResolveTaskCredential(ctx, channel, &stored)
			require.ErrorIs(t, err, ErrGatewayStateUnavailable)
			channel.ChannelInfo.MultiKeyStatusList = nil
			changedEndpoint := "https://other.example.test"
			channel.BaseURL = &changedEndpoint
			_, _, err = ResolveTaskCredential(ctx, channel, &stored)
			require.ErrorIs(t, err, ErrGatewayStateUnavailable)
			channel.BaseURL = nil

			channel.Key = "replacement\nfirst"
			require.NoError(t, channel.Update())
			_, _, err = ResolveTaskCredential(ctx, channel, &stored)
			require.ErrorIs(t, err, ErrGatewayStateUnavailable)
			channel.Key = "second\nfirst"
			require.NoError(t, channel.Update())
			_, _, err = ResolveTaskCredential(ctx, channel, &stored)
			require.ErrorIs(t, err, ErrGatewayStateUnavailable, "restoring an old key must not revive its old task binding")

			refreshChannel := &Channel{Key: "original", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}
			require.NoError(t, db.Create(refreshChannel).Error)
			refreshTask := &Task{ChannelId: refreshChannel.Id}
			require.NoError(t, refreshTask.PrivateData.BindCredential(ctx, refreshChannel, "original"))
			refreshChannel.Key = "replacement"
			require.NoError(t, refreshChannel.Update())
			require.NoError(t, db.First(refreshChannel, refreshChannel.Id).Error)
			_, _, err = ResolveTaskCredential(ctx, refreshChannel, refreshTask)
			require.ErrorIs(t, err, ErrGatewayStateUnavailable)
			_, _, err = ResolveTaskCredential(ctx, channel, refreshTask)
			require.ErrorIs(t, err, ErrGatewayStateUnavailable)
		})
	}
}

func TestTaskJSONDatabaseValues(t *testing.T) {
	for _, value := range []any{nil, []byte{}, "", "null", []byte("null")} {
		properties := Properties{Input: "stale"}
		privateData := TaskPrivateData{Key: "stale", CredentialID: "stale"}
		require.NoError(t, properties.Scan(value))
		require.NoError(t, privateData.Scan(value))
		assert.Equal(t, Properties{}, properties)
		assert.Equal(t, TaskPrivateData{}, privateData)
	}
	for _, value := range []any{true, 42, "{"} {
		assert.Error(t, (&Properties{}).Scan(value))
		assert.Error(t, (&TaskPrivateData{}).Scan(value))
	}
	properties := Properties{Input: "input"}
	propertyValue, err := properties.Value()
	require.NoError(t, err)
	propertyText, ok := propertyValue.(string)
	require.True(t, ok, "JSON parameters must be text in PostgreSQL simple protocol")
	for _, value := range []any{propertyText, []byte(propertyText)} {
		var decoded Properties
		require.NoError(t, decoded.Scan(value))
		assert.Equal(t, properties, decoded)
	}
	privateData := TaskPrivateData{CredentialVersion: 1, CredentialID: "identity", EndpointID: "endpoint"}
	privateValue, err := privateData.Value()
	require.NoError(t, err)
	privateText, ok := privateValue.(string)
	require.True(t, ok, "JSON parameters must be text in PostgreSQL simple protocol")
	for _, value := range []any{privateText, []byte(privateText)} {
		var decoded TaskPrivateData
		require.NoError(t, decoded.Scan(value))
		assert.Equal(t, privateData, decoded)
	}
}

func TestResolveTaskChannelKeyUsesReferenceAndLegacyFallback(t *testing.T) {
	channel := &Channel{
		Key: "first\nsecond",
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
		},
	}

	assert.Equal(t, "second", ResolveTaskChannelKey(channel, &Task{
		PrivateData: TaskPrivateData{ChannelKeyIndex: 1},
	}))
	assert.Equal(t, "legacy", ResolveTaskChannelKey(channel, &Task{
		PrivateData: TaskPrivateData{Key: " legacy ", ChannelKeyIndex: 1},
	}))
	assert.Empty(t, ResolveTaskChannelKey(channel, &Task{
		PrivateData: TaskPrivateData{ChannelKeyIndex: 5},
	}))
}
