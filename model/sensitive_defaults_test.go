package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestSensitiveDefaultsPreserveAdministratorConfiguration(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	oldEnabled, oldPrompt, oldWords, oldOptions := setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled, setting.SensitiveWords, common.OptionMap
	t.Cleanup(func() {
		setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled, setting.SensitiveWords = oldEnabled, oldPrompt, oldWords
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled, setting.SensitiveWords = false, false, nil
	require.NoError(t, db.Create(&[]Option{
		{Key: "CheckSensitiveEnabled", Value: "true"},
		{Key: "CheckSensitiveOnPromptEnabled", Value: "true"},
		{Key: "SensitiveWords", Value: "administrator-word\nretained-word"},
	}).Error)
	InitOptionMap()
	require.True(t, setting.ShouldCheckPromptSensitive())
	require.Equal(t, []string{"administrator-word", "retained-word"}, setting.SensitiveWords)
	InitOptionMap()
	require.True(t, setting.ShouldCheckPromptSensitive(), "reinitialization keeps saved policy")
	require.Equal(t, "administrator-word\nretained-word", requireOptionValue(t, db, "SensitiveWords"))
	require.NoError(t, UpdateOptionsBulk(map[string]string{"CheckSensitiveEnabled": "false", "SensitiveWords": ""}))
	loadOptionsFromDatabase()
	require.False(t, setting.ShouldCheckPromptSensitive())
	require.Empty(t, setting.SensitiveWords)
}
