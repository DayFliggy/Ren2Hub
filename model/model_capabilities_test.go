package model

import (
	"errors"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestModelCapabilitiesAcrossDatabases(t *testing.T) {
	for _, dialect := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL} {
		t.Run(string(dialect), func(t *testing.T) {
			dsn := ""
			if dialect != common.DatabaseTypeSQLite {
				env := "TEST_MYSQL_DSN"
				if dialect == common.DatabaseTypePostgreSQL {
					env = "TEST_POSTGRES_DSN"
				}
				dsn = os.Getenv(env)
				if dsn == "" {
					t.Skip(env + " is not configured; database compatibility remains unverified")
				}
			}
			db, err := gatewayStartupDatabase(t, dialect, dsn)()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, connection.Close()) })
			previousDB := DB
			DB = db
			t.Cleanup(func() { DB = previousDB })
			require.NoError(t, db.AutoMigrate(&Model{}))

			unknown := Model{ModelName: "unknown-card", Status: 1}
			require.NoError(t, unknown.Insert())
			var loaded Model
			require.NoError(t, db.First(&loaded, unknown.Id).Error)
			assert.Nil(t, loaded.Capabilities, "empty databases do not invent model capabilities")

			toolCall, contextLimit, outputLimit, source := false, int64(128000), int64(16000), "administrator"
			loaded.Capabilities = &ModelCapabilities{
				ReasoningEfforts: []string{}, InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"},
				ContextLimit: &contextLimit, OutputLimit: &outputLimit, ToolCall: &toolCall, Source: &source,
			}
			require.NoError(t, loaded.Update())
			var known Model
			require.NoError(t, db.First(&known, unknown.Id).Error)
			require.NotNil(t, known.Capabilities)
			assert.NotNil(t, known.Capabilities.ReasoningEfforts, "explicit empty capability lists survive database round trips")
			assert.Equal(t, []string{"text", "image"}, known.Capabilities.InputModalities)
			require.NotNil(t, known.Capabilities.ToolCall)
			assert.False(t, *known.Capabilities.ToolCall, "explicit false must not become unknown")
			assert.Equal(t, contextLimit, *known.Capabilities.ContextLimit)
			assert.Positive(t, known.Capabilities.UpdatedAt)

			// The existing metadata editor does not know capability fields yet.
			known.Capabilities = nil
			known.Description = "metadata-only change"
			require.NoError(t, known.Update())
			require.NoError(t, db.First(&known, unknown.Id).Error)
			require.NotNil(t, known.Capabilities)
			assert.False(t, *known.Capabilities.ToolCall)

			invalidLimit := int64(-1)
			known.Capabilities.ContextLimit = &invalidLimit
			known.Description = "must not persist"
			require.Error(t, known.Update())
			var unchanged Model
			require.NoError(t, db.First(&unchanged, unknown.Id).Error)
			assert.Equal(t, "metadata-only change", unchanged.Description)
			assert.Equal(t, contextLimit, *unchanged.Capabilities.ContextLimit)

			unchanged.Capabilities = &ModelCapabilities{}
			require.NoError(t, unchanged.Update())
			require.NoError(t, db.First(&unchanged, unknown.Id).Error)
			assert.Nil(t, unchanged.Capabilities.ToolCall)
			assert.Nil(t, unchanged.Capabilities.ReasoningEfforts)
			require.NoError(t, unchanged.Delete())
			var count int64
			require.NoError(t, db.Model(&Model{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.Migrator().DropTable(&Model{}))
		})
	}
}

func TestModelInsertRollsBackCapabilityAndMetadataTogether(t *testing.T) {
	db, err := gatewayStartupDatabase(t, common.DatabaseTypeSQLite, "")()
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	require.NoError(t, db.AutoMigrate(&Model{}))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:reject_model_status_update", func(tx *gorm.DB) {
		tx.AddError(errors.New("write rejected"))
	}))
	m := Model{ModelName: "atomic-capability", Capabilities: &ModelCapabilities{}, Status: 0}
	require.ErrorContains(t, m.Insert(), "write rejected")
	var count int64
	require.NoError(t, db.Model(&Model{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestModelCapabilitiesRejectInvalidValuesWithoutChangingScannedState(t *testing.T) {
	toolCall := true
	card := ModelCapabilities{ToolCall: &toolCall}
	for _, input := range []any{42, "not json", `{"context_limit":0}`, `{"reasoning_efforts":["high","high"]}`} {
		require.Error(t, card.Scan(input))
		require.NotNil(t, card.ToolCall)
		assert.True(t, *card.ToolCall)
	}
	require.NoError(t, card.Scan(`{"tool_call":false,"input_modalities":[],"reasoning_efforts":null}`))
	assert.False(t, *card.ToolCall)
	assert.NotNil(t, card.InputModalities)
	assert.Nil(t, card.ReasoningEfforts)
	encoded, err := card.Value()
	require.NoError(t, err)
	assert.IsType(t, "", encoded)
}
